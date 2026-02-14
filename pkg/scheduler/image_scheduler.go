package scheduler

import (
	"context"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ImageScheduler watches Node resources and provides image-locality-aware
// node selection via Rendezvous (HRW) hashing.
type ImageScheduler struct {
	client client.Client

	mu    sync.RWMutex
	nodes []string // schedulable node names
}

// NewImageScheduler creates a new ImageScheduler.
func NewImageScheduler(c client.Client) *ImageScheduler {
	return &ImageScheduler{
		client: c,
	}
}

// Reconcile handles Node create/update/delete events to maintain the
// cached list of schedulable nodes.
func (s *ImageScheduler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("node", req.Name)

	node := &corev1.Node{}
	if err := s.client.Get(ctx, req.NamespacedName, node); err != nil {
		if errors.IsNotFound(err) {
			s.removeNode(req.Name)
			logger.V(1).Info("removed deleted node from scheduler cache")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if isSchedulable(node) {
		s.addNode(req.Name)
	} else {
		s.removeNode(req.Name)
	}

	return ctrl.Result{}, nil
}

// SelectNodes returns the top-k preferred nodes for the given image
// using Rendezvous hashing over the current set of schedulable nodes.
func (s *ImageScheduler) SelectNodes(image string, k int) []string {
	s.mu.RLock()
	nodes := make([]string, len(s.nodes))
	copy(nodes, s.nodes)
	s.mu.RUnlock()

	return ComputeTopK(image, nodes, k)
}

// SetupWithManager registers this scheduler as a controller watching Node objects.
func (s *ImageScheduler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("image-scheduler").
		For(&corev1.Node{}).
		Complete(s)
}

// isSchedulable returns true if the node is Ready and not cordoned.
func isSchedulable(node *corev1.Node) bool {
	if node.Spec.Unschedulable {
		return false
	}
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

func (s *ImageScheduler) addNode(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, n := range s.nodes {
		if n == name {
			return
		}
	}
	s.nodes = append(s.nodes, name)
}

func (s *ImageScheduler) removeNode(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, n := range s.nodes {
		if n == name {
			s.nodes = append(s.nodes[:i], s.nodes[i+1:]...)
			return
		}
	}
}
