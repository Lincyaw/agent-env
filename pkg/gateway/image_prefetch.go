package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	prefetchTimeout        = 10 * time.Minute
	prefetchPollPeriod     = 5 * time.Second
	prefetchLabelKey       = "agent-env.io/prefetch-pool"
	prefetchMaxConcurrency = 20
)

// prefetchSem limits how many prefetch pods run simultaneously.
var prefetchSem = make(chan struct{}, prefetchMaxConcurrency)

func prefetchPodName(poolName string) string {
	return dnsLabelWithSuffix(poolName, "-prefetch")
}

// PrefetchImage triggers image prefetch for an existing pool. It resolves
// the pool's primary image from the template, then creates a single
// ephemeral pod to pull it onto one node. The image-locality scheduler
// will direct future pods to that node.
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

// runImagePrefetch creates a single ephemeral pod to pull the image onto
// one cluster node. The image-locality scheduler handles spreading from there.
func (g *Gateway) runImagePrefetch(poolName, namespace, image string) {
	prefetchSem <- struct{}{}
	defer func() { <-prefetchSem }()

	log.Printf("prefetch: started for pool %s/%s image %s", namespace, poolName, image)

	ctx, cancel := context.WithTimeout(context.Background(), prefetchTimeout)
	defer cancel()

	podName := prefetchPodName(poolName)
	pod := buildPrefetchPod(podName, namespace, poolName, image)

	if err := g.k8sClient.Create(ctx, pod); err != nil {
		if errors.IsAlreadyExists(err) {
			log.Printf("prefetch: pod %s/%s already exists, reusing", namespace, podName)
		} else {
			log.Printf("prefetch: failed to create pod %s/%s: %v", namespace, podName, err)
			return
		}
	}

	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := g.deletePrefetchPod(cleanupCtx, podName, namespace); err != nil {
			log.Printf("prefetch: failed to delete pod %s/%s: %v", namespace, podName, err)
		}
	}()

	if err := g.waitForPrefetchPod(ctx, podName, namespace); err != nil {
		log.Printf("prefetch: failed for pool %s/%s: %v", namespace, poolName, err)
		return
	}

	log.Printf("prefetch: complete for pool %s/%s image %s", namespace, poolName, image)
}

func buildPrefetchPod(name, namespace, poolName, image string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{prefetchLabelKey: poolName},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "prefetch",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"true"},
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
			TerminationGracePeriodSeconds: int64Ptr(0),
			AutomountServiceAccountToken:  boolPtr(false),
		},
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func (g *Gateway) waitForPrefetchPod(ctx context.Context, podName, namespace string) error {
	key := types.NamespacedName{Name: podName, Namespace: namespace}
	ticker := time.NewTicker(prefetchPollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("prefetch timed out: %w", ctx.Err())
		case <-ticker.C:
			pod := &corev1.Pod{}
			if err := g.k8sClient.Get(ctx, key, pod); err != nil {
				if errors.IsNotFound(err) {
					return fmt.Errorf("prefetch pod disappeared")
				}
				continue
			}

			switch pod.Status.Phase {
			case corev1.PodSucceeded:
				return nil
			case corev1.PodFailed:
				return fmt.Errorf("prefetch pod failed")
			}

			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting == nil {
					continue
				}
				reason := cs.State.Waiting.Reason
				if strings.Contains(reason, "ErrImagePull") || strings.Contains(reason, "ImagePullBackOff") {
					return fmt.Errorf("image pull failed: %s: %s", reason, cs.State.Waiting.Message)
				}
			}
		}
	}
}

func (g *Gateway) deletePrefetchPod(ctx context.Context, name, namespace string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
	}
	if err := g.k8sClient.Delete(ctx, pod, &client.DeleteOptions{
		GracePeriodSeconds: int64Ptr(0),
	}); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}
