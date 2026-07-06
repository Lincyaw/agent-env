package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prefetchTimeout    = 10 * time.Minute
	prefetchPollPeriod = 5 * time.Second
	prefetchLabelKey   = "agent-env.io/prefetch-pool"
)

// prefetchDaemonSetName returns a DNS-safe DaemonSet name for the pool.
func prefetchDaemonSetName(poolName string) string {
	return dnsLabelWithSuffix(poolName, "-prefetch")
}

// PrefetchImage triggers an image prefetch for an existing pool. It looks up
// the pool's template to discover the primary image, then runs a prefetch
// DaemonSet in the background.
func (g *Gateway) PrefetchImage(ctx context.Context, poolName, namespace string) error {
	ns, err := g.resolveNamespace(namespace)
	if err != nil {
		return err
	}

	image, err := g.poolPrimaryImage(ctx, poolName, ns)
	if err != nil {
		return err
	}

	go g.runImagePrefetch(poolName, ns, image)
	return nil
}

// poolPrimaryImage resolves the primary executor image from a pool's template.
func (g *Gateway) poolPrimaryImage(ctx context.Context, poolName, namespace string) (string, error) {
	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		return "", fmt.Errorf("get pool %s/%s for prefetch: %w", namespace, poolName, err)
	}
	templateName := pool.Spec.TemplateRef.Name
	if templateName == "" {
		return "", fmt.Errorf("pool %s/%s has no templateRef", namespace, poolName)
	}
	template := &extensionsv1beta1.SandboxTemplate{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: templateName, Namespace: namespace}, template); err != nil {
		return "", fmt.Errorf("get template %s/%s for prefetch: %w", namespace, templateName, err)
	}
	image := primarySandboxTemplateImage(template)
	if image == "" {
		return "", fmt.Errorf("template %s/%s has no primary image", namespace, templateName)
	}
	return image, nil
}

// runImagePrefetch creates an ephemeral DaemonSet to pull the image onto every
// node, waits for completion, then deletes the DaemonSet.
func (g *Gateway) runImagePrefetch(poolName, namespace, image string) {
	log.Printf("prefetch: started for pool %s/%s image %s", namespace, poolName, image)

	ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
	defer cancel()

	dsName := prefetchDaemonSetName(poolName)
	ds := g.buildPrefetchDaemonSet(dsName, namespace, poolName, image)

	if err := g.k8sClient.Create(ctx, ds); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Printf("prefetch: DaemonSet %s/%s already exists, reusing", namespace, dsName)
		} else {
			log.Printf("prefetch: failed to create DaemonSet %s/%s: %v", namespace, dsName, err)
			return
		}
	}

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := g.deletePrefetchDaemonSet(cleanupCtx, dsName, namespace); err != nil {
			log.Printf("prefetch: failed to delete DaemonSet %s/%s: %v", namespace, dsName, err)
		}
	}()

	if err := g.waitForPrefetchComplete(ctx, dsName, namespace, poolName); err != nil {
		log.Printf("prefetch: failed for pool %s/%s: %v", namespace, poolName, err)
		return
	}

	log.Printf("prefetch: complete for pool %s/%s image %s", namespace, poolName, image)
}

func (g *Gateway) buildPrefetchDaemonSet(name, namespace, poolName, image string) *appsv1.DaemonSet {
	labels := map[string]string{
		prefetchLabelKey: poolName,
	}
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					// Minimal pod that pulls the image then exits quickly.
					Containers: []corev1.Container{
						{
							Name:            "prefetch",
							Image:           image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"sh", "-c", "sleep 5"},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("10m"),
									corev1.ResourceMemory: resource.MustParse("16Mi"),
								},
							},
						},
					},
					// Tolerate all taints so prefetch reaches every node.
					Tolerations: []corev1.Toleration{
						{Operator: corev1.TolerationOpExists},
					},
					TerminationGracePeriodSeconds: int64Ptr(0),
					AutomountServiceAccountToken:  boolPtr(false),
				},
			},
		},
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

// waitForPrefetchComplete polls the DaemonSet until all desired pods have
// pulled the image (status is Running or later, not stuck in
// ContainerCreating/ErrImagePull).
func (g *Gateway) waitForPrefetchComplete(ctx context.Context, dsName, namespace, poolName string) error {
	key := types.NamespacedName{Name: dsName, Namespace: namespace}
	ticker := time.NewTicker(prefetchPollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("prefetch timed out: %w", ctx.Err())
		case <-ticker.C:
			ds := &appsv1.DaemonSet{}
			if err := g.k8sClient.Get(ctx, key, ds); err != nil {
				return fmt.Errorf("get DaemonSet: %w", err)
			}

			desired := ds.Status.DesiredNumberScheduled
			if desired == 0 {
				// Scheduler hasn't evaluated yet.
				continue
			}

			ready := ds.Status.NumberReady
			log.Printf("prefetch: %s/%s progress %d/%d nodes ready", namespace, dsName, ready, desired)

			if ready >= desired {
				return nil
			}

			// Check for pods stuck in image pull errors.
			if err := g.checkPrefetchPodErrors(ctx, namespace, poolName); err != nil {
				return err
			}
		}
	}
}

// checkPrefetchPodErrors scans prefetch pods for unrecoverable image pull
// failures.
func (g *Gateway) checkPrefetchPodErrors(ctx context.Context, namespace, poolName string) error {
	var pods corev1.PodList
	if err := g.k8sClient.List(ctx, &pods,
		client.InNamespace(namespace),
		client.MatchingLabels{prefetchLabelKey: poolName},
	); err != nil {
		return nil // non-fatal: we'll catch it on next poll
	}

	for i := range pods.Items {
		pod := &pods.Items[i]
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				if strings.Contains(reason, "ErrImagePull") || strings.Contains(reason, "ImagePullBackOff") {
					return fmt.Errorf("image pull failed on node %s: %s: %s",
						pod.Spec.NodeName, reason, cs.State.Waiting.Message)
				}
			}
		}
	}
	return nil
}

func (g *Gateway) deletePrefetchDaemonSet(ctx context.Context, name, namespace string) error {
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}
	propagation := metav1.DeletePropagationForeground
	if err := g.k8sClient.Delete(ctx, ds, &client.DeleteOptions{
		PropagationPolicy: &propagation,
	}); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}
