package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/sidecar"
)

const (
	SidecarPort       = 8080
	WorkspaceDir      = "/workspace"
	PoolLabelKey      = "arl.infra.io/pool"
	SandboxLabelKey   = "arl.infra.io/sandbox"
	StatusLabelKey    = "arl.infra.io/status"
	StatusIdle        = "idle"
	StatusAllocated   = "allocated"
)

// WarmPoolReconciler reconciles a WarmPool object
type WarmPoolReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=arl.infra.io,resources=warmpools/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete

// Reconcile manages the WarmPool lifecycle
func (r *WarmPoolReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Fetch the WarmPool instance
	pool := &arlv1alpha1.WarmPool{}
	if err := r.Get(ctx, req.NamespacedName, pool); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// List all pods belonging to this pool
	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, 
		client.InNamespace(req.Namespace),
		client.MatchingLabels{PoolLabelKey: pool.Name}); err != nil {
		return ctrl.Result{}, err
	}

	// Count idle and allocated pods
	var readyIdle, allocated int32
	for _, pod := range podList.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}
		status := pod.Labels[StatusLabelKey]
		if status == StatusIdle && pod.Status.Phase == corev1.PodRunning {
			readyIdle++
		} else if status == StatusAllocated {
			allocated++
		}
	}

	// Calculate how many pods to create
	totalPods := readyIdle + allocated
	needed := pool.Spec.Replicas - readyIdle

	logger.Info("Pool status", 
		"pool", pool.Name,
		"desired", pool.Spec.Replicas,
		"ready", readyIdle,
		"allocated", allocated,
		"total", totalPods,
		"needed", needed)

	// Create new pods if needed
	if needed > 0 {
		for i := int32(0); i < needed; i++ {
			pod := r.constructPod(pool)
			if err := r.Create(ctx, pod); err != nil {
				logger.Error(err, "Failed to create pod")
				continue
			}
			logger.Info("Created pod", "pod", pod.Name)
		}
	}

	// Update status
	pool.Status.ReadyReplicas = readyIdle
	pool.Status.AllocatedReplicas = allocated
	if err := r.Status().Update(ctx, pool); err != nil {
		return ctrl.Result{}, err
	}

	// Requeue to maintain the pool
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

// constructPod creates a Pod from the WarmPool template
func (r *WarmPoolReconciler) constructPod(pool *arlv1alpha1.WarmPool) *corev1.Pod {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: pool.Name + "-",
			Namespace:    pool.Namespace,
			Labels: map[string]string{
				PoolLabelKey:   pool.Name,
				StatusLabelKey: StatusIdle,
			},
		},
		Spec: pool.Spec.Template.Spec,
	}

	// Ensure sidecar container exists
	hasSidecar := false
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "sidecar" {
			hasSidecar = true
			break
		}
	}

	if !hasSidecar {
		// Add default sidecar container
		sidecarContainer := corev1.Container{
			Name:  "sidecar",
			Image: "arl-sidecar:latest",
			ImagePullPolicy: corev1.PullIfNotPresent,
			Ports: []corev1.ContainerPort{
				{
					ContainerPort: SidecarPort,
					Protocol:      corev1.ProtocolTCP,
				},
			},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "workspace",
					MountPath: WorkspaceDir,
				},
			},
		}
		pod.Spec.Containers = append(pod.Spec.Containers, sidecarContainer)
	}

	// Add shared workspace volume if not exists
	hasWorkspace := false
	for _, vol := range pod.Spec.Volumes {
		if vol.Name == "workspace" {
			hasWorkspace = true
			break
		}
	}

	if !hasWorkspace {
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name: "workspace",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Set owner reference
	ctrl.SetControllerReference(pool, pod, r.Scheme)

	return pod
}

// SetupWithManager sets up the controller with the Manager
func (r *WarmPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&arlv1alpha1.WarmPool{}).
		Owns(&corev1.Pod{}).
		Complete(r)
}

// SidecarClient provides methods to communicate with the sidecar
type SidecarClient struct {
	httpClient *http.Client
}

// NewSidecarClient creates a new sidecar client
func NewSidecarClient() *SidecarClient {
	return &SidecarClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// UpdateFiles sends file update request to sidecar
func (c *SidecarClient) UpdateFiles(podIP string, req *sidecar.FileRequest) (*sidecar.FileResponse, error) {
	url := fmt.Sprintf("http://%s:%d/files", podIP, SidecarPort)
	resp := &sidecar.FileResponse{}
	if err := c.doRequest(url, req, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Execute sends execute request to sidecar
func (c *SidecarClient) Execute(podIP string, req *sidecar.ExecRequest) (*sidecar.ExecLog, error) {
	url := fmt.Sprintf("http://%s:%d/execute", podIP, SidecarPort)
	resp := &sidecar.ExecLog{}
	if err := c.doRequest(url, req, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// Reset sends reset request to sidecar
func (c *SidecarClient) Reset(podIP string, req *sidecar.ResetRequest) (*sidecar.ResetResponse, error) {
	url := fmt.Sprintf("http://%s:%d/reset", podIP, SidecarPort)
	resp := &sidecar.ResetResponse{}
	if err := c.doRequest(url, req, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// doRequest performs HTTP request to sidecar
func (c *SidecarClient) doRequest(url string, reqBody interface{}, respBody interface{}) error {
	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := c.httpClient.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(body))
	}

	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	return nil
}
