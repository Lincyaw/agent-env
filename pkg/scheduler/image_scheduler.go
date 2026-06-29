package scheduler

import (
	"context"
	"sort"
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

	mu         sync.RWMutex
	nodes      []string // schedulable node names
	nodeImages map[string]map[string]struct{}
	imageNodes map[string]map[string]struct{}
}

// NewImageScheduler creates a new ImageScheduler.
func NewImageScheduler(c client.Client) *ImageScheduler {
	return &ImageScheduler{
		client:     c,
		nodeImages: make(map[string]map[string]struct{}),
		imageNodes: make(map[string]map[string]struct{}),
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
		s.upsertNode(node)
	} else {
		s.removeNode(req.Name)
	}

	return ctrl.Result{}, nil
}

// SelectNodes returns the top-k preferred nodes for the given image
// using Rendezvous hashing over the current set of schedulable nodes.
func (s *ImageScheduler) SelectNodes(image string, k int) []string {
	s.mu.RLock()
	nodes := s.nodesForImageLocked(image)
	s.mu.RUnlock()

	return ComputeTopK(image, nodes, k)
}

// CachedNodesForImage returns schedulable nodes that currently report the image
// in their kubelet image cache.
func (s *ImageScheduler) CachedNodesForImage(image string) []string {
	s.mu.RLock()
	nodes := sortedSetValues(s.imageNodes[image])
	s.mu.RUnlock()
	return nodes
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

func (s *ImageScheduler) upsertNode(node *corev1.Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name := node.Name
	for _, n := range s.nodes {
		if n == name {
			s.updateNodeImagesLocked(name, nodeImageNames(node))
			return
		}
	}
	s.nodes = append(s.nodes, name)
	s.updateNodeImagesLocked(name, nodeImageNames(node))
}

func (s *ImageScheduler) removeNode(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, n := range s.nodes {
		if n == name {
			s.nodes = append(s.nodes[:i], s.nodes[i+1:]...)
			break
		}
	}
	s.removeNodeImagesLocked(name)
}

func (s *ImageScheduler) nodesForImageLocked(image string) []string {
	if nodes := sortedSetValues(s.imageNodes[image]); len(nodes) > 0 {
		return nodes
	}
	nodes := make([]string, len(s.nodes))
	copy(nodes, s.nodes)
	return nodes
}

func (s *ImageScheduler) updateNodeImagesLocked(nodeName string, images map[string]struct{}) {
	s.removeNodeImagesLocked(nodeName)
	s.nodeImages[nodeName] = images
	for image := range images {
		nodes := s.imageNodes[image]
		if nodes == nil {
			nodes = make(map[string]struct{})
			s.imageNodes[image] = nodes
		}
		nodes[nodeName] = struct{}{}
	}
}

func (s *ImageScheduler) removeNodeImagesLocked(nodeName string) {
	for image := range s.nodeImages[nodeName] {
		nodes := s.imageNodes[image]
		delete(nodes, nodeName)
		if len(nodes) == 0 {
			delete(s.imageNodes, image)
		}
	}
	delete(s.nodeImages, nodeName)
}

func nodeImageNames(node *corev1.Node) map[string]struct{} {
	images := make(map[string]struct{})
	for _, image := range node.Status.Images {
		for _, name := range image.Names {
			if name != "" {
				images[name] = struct{}{}
			}
		}
	}
	return images
}

func sortedSetValues(values map[string]struct{}) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
