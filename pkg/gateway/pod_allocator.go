package gateway

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/interfaces"
	"github.com/Lincyaw/agent-env/pkg/labels"
)

// podQueue is a FIFO queue of idle pods for a single pool.
type podQueue struct {
	mu   sync.Mutex
	pods []*corev1.Pod
}

func (q *podQueue) push(pod *corev1.Pod) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.pods = append(q.pods, pod)
}

func (q *podQueue) pop() *corev1.Pod {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.pods) == 0 {
		return nil
	}
	pod := q.pods[0]
	q.pods = q.pods[1:]
	return pod
}

func (q *podQueue) remove(podName string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, p := range q.pods {
		if p.Name == podName {
			q.pods = append(q.pods[:i], q.pods[i+1:]...)
			return
		}
	}
}

func (q *podQueue) len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pods)
}

// waiter represents a blocked Allocate caller waiting for an idle pod.
type waiter struct {
	ch chan *corev1.Pod
}

// waiterList is a FIFO list of blocked callers for a single pool.
type waiterList struct {
	mu      sync.Mutex
	waiters []*waiter
}

func (wl *waiterList) add() *waiter {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	w := &waiter{ch: make(chan *corev1.Pod, 1)}
	wl.waiters = append(wl.waiters, w)
	return w
}

func (wl *waiterList) wakeFirst(pod *corev1.Pod) bool {
	wl.mu.Lock()
	defer wl.mu.Unlock()
	if len(wl.waiters) == 0 {
		return false
	}
	w := wl.waiters[0]
	wl.waiters = wl.waiters[1:]
	w.ch <- pod
	return true
}

// PodAllocator watches pods via an Informer-backed cache and provides
// FIFO allocation of idle pods from warm pools.
type PodAllocator struct {
	k8sClient client.Client
	podCache  cache.Cache
	idlePods  sync.Map // "ns/poolName" -> *podQueue
	waiters   sync.Map // "ns/poolName" -> *waiterList
	metrics   interfaces.MetricsCollector
	stopCh    chan struct{}
}

// NewPodAllocator creates a new PodAllocator with a controller-runtime cache for Pods.
func NewPodAllocator(k8sClient client.Client, restConfig *rest.Config, scheme *runtime.Scheme, metrics interfaces.MetricsCollector) (*PodAllocator, error) {
	podCache, err := cache.New(restConfig, cache.Options{
		Scheme: scheme,
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Pod{}: {},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create pod cache: %w", err)
	}

	return &PodAllocator{
		k8sClient: k8sClient,
		podCache:  podCache,
		metrics:   metrics,
		stopCh:    make(chan struct{}),
	}, nil
}

// Start starts the cache, waits for sync, registers event handlers, and populates initial idle queues.
func (pa *PodAllocator) Start(ctx context.Context) error {
	go func() {
		if err := pa.podCache.Start(ctx); err != nil {
			log.Printf("PodAllocator cache error: %v", err)
		}
	}()

	if !pa.podCache.WaitForCacheSync(ctx) {
		return fmt.Errorf("pod cache failed to sync")
	}

	// Register event handler on Pod informer
	podInformer, err := pa.podCache.GetInformer(ctx, &corev1.Pod{})
	if err != nil {
		return fmt.Errorf("get pod informer: %w", err)
	}

	podInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return
			}
			pa.handlePodEvent(pod)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			pod, ok := newObj.(*corev1.Pod)
			if !ok {
				return
			}
			pa.handlePodEvent(pod)
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return
			}
			pa.handlePodDelete(pod)
		},
	})

	// Populate initial idle queues from existing pods
	var podList corev1.PodList
	if err := pa.podCache.List(ctx, &podList); err != nil {
		return fmt.Errorf("list pods for initial population: %w", err)
	}
	for i := range podList.Items {
		pod := &podList.Items[i]
		if pa.isPodIdleAndReady(pod) {
			key := pa.queueKey(pod)
			if key != "" {
				q := pa.getOrCreateQueue(key)
				q.push(pod.DeepCopy())
			}
		}
	}

	log.Printf("PodAllocator started (cached %d pods)", len(podList.Items))
	return nil
}

// Stop signals the allocator to shut down.
func (pa *PodAllocator) Stop() {
	close(pa.stopCh)
}

// Allocate dequeues an idle pod from the given pool or blocks until one is available.
// On success, it patches the pod's labels to mark it as allocated (optimistic concurrency).
func (pa *PodAllocator) Allocate(ctx context.Context, poolName, namespace string) (*corev1.Pod, error) {
	start := time.Now()
	key := namespace + "/" + poolName

	for {
		// Try to dequeue from the idle queue
		q := pa.getOrCreateQueue(key)
		for {
			pod := q.pop()
			if pod == nil {
				break
			}

			// Attempt to claim the pod via label patch with optimistic concurrency
			claimed, err := pa.claimPod(ctx, pod)
			if err != nil {
				log.Printf("Warning: failed to claim pod %s: %v", pod.Name, err)
				continue
			}
			if claimed {
				if pa.metrics != nil {
					pa.metrics.RecordPodAllocationDuration(poolName, time.Since(start))
					pa.metrics.IncrementPodAllocationResult(poolName, "success")
				}
				return pod, nil
			}
			// Another allocator won the race; try the next pod
		}

		// No idle pods available; register as a waiter
		wl := pa.getOrCreateWaiterList(key)
		w := wl.add()

		select {
		case pod := <-w.ch:
			// Got woken up with a pod; try to claim it
			claimed, err := pa.claimPod(ctx, pod)
			if err != nil {
				log.Printf("Warning: failed to claim pod %s from waiter: %v", pod.Name, err)
				continue // retry the whole loop
			}
			if claimed {
				if pa.metrics != nil {
					pa.metrics.RecordPodAllocationDuration(poolName, time.Since(start))
					pa.metrics.IncrementPodAllocationResult(poolName, "success")
				}
				return pod, nil
			}
			// Lost the race; loop again
		case <-ctx.Done():
			if pa.metrics != nil {
				pa.metrics.RecordPodAllocationDuration(poolName, time.Since(start))
				pa.metrics.IncrementPodAllocationResult(poolName, "timeout")
			}
			return nil, fmt.Errorf("allocate pod from pool %s: %w", poolName, ctx.Err())
		}
	}
}

// Release deletes a pod directly. The WarmPoolController will create a replacement.
func (pa *PodAllocator) Release(ctx context.Context, podName, namespace string) error {
	pod := &corev1.Pod{}
	pod.Name = podName
	pod.Namespace = namespace
	if err := pa.k8sClient.Delete(ctx, pod); err != nil {
		return fmt.Errorf("delete pod %s/%s: %w", namespace, podName, err)
	}
	return nil
}

// claimPod patches the pod's status label from idle to allocated using optimistic concurrency.
func (pa *PodAllocator) claimPod(ctx context.Context, pod *corev1.Pod) (bool, error) {
	// Re-read the pod to get current resource version
	current := &corev1.Pod{}
	if err := pa.k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), current); err != nil {
		return false, err
	}

	// Verify the pod is still idle
	if current.Labels[labels.StatusLabelKey] != labels.StatusIdle {
		return false, nil
	}
	if current.DeletionTimestamp != nil {
		return false, nil
	}

	// Patch labels: status -> allocated
	patch := client.MergeFrom(current.DeepCopy())
	if current.Labels == nil {
		current.Labels = make(map[string]string)
	}
	current.Labels[labels.StatusLabelKey] = labels.StatusAllocated
	if err := pa.k8sClient.Patch(ctx, current, patch); err != nil {
		return false, err
	}

	// Update the returned pod with the latest state
	*pod = *current
	return true, nil
}

// handlePodEvent processes a pod add/update event.
func (pa *PodAllocator) handlePodEvent(pod *corev1.Pod) {
	if !pa.isPodIdleAndReady(pod) {
		return
	}

	key := pa.queueKey(pod)
	if key == "" {
		return
	}

	podCopy := pod.DeepCopy()

	// Try to wake a waiter first
	if wlVal, ok := pa.waiters.Load(key); ok {
		wl := wlVal.(*waiterList)
		if wl.wakeFirst(podCopy) {
			return
		}
	}

	// No waiters; add to queue
	q := pa.getOrCreateQueue(key)
	q.push(podCopy)
}

// handlePodDelete removes a pod from the idle queue if it's there.
func (pa *PodAllocator) handlePodDelete(pod *corev1.Pod) {
	key := pa.queueKey(pod)
	if key == "" {
		return
	}

	if qVal, ok := pa.idlePods.Load(key); ok {
		q := qVal.(*podQueue)
		q.remove(pod.Name)
	}
}

// isPodIdleAndReady returns true if the pod has the idle status label, is Running,
// has no deletion timestamp, and all containers are ready.
func (pa *PodAllocator) isPodIdleAndReady(pod *corev1.Pod) bool {
	if pod.Labels[labels.StatusLabelKey] != labels.StatusIdle {
		return false
	}
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	if pod.DeletionTimestamp != nil {
		return false
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if !cs.Ready {
			return false
		}
	}
	return true
}

// queueKey returns the queue key "namespace/poolName" for a pod, or empty string
// if the pod doesn't belong to a pool.
func (pa *PodAllocator) queueKey(pod *corev1.Pod) string {
	poolName := pod.Labels[labels.PoolLabelKey]
	if poolName == "" {
		return ""
	}
	return pod.Namespace + "/" + poolName
}

func (pa *PodAllocator) getOrCreateQueue(key string) *podQueue {
	val, _ := pa.idlePods.LoadOrStore(key, &podQueue{})
	return val.(*podQueue)
}

func (pa *PodAllocator) getOrCreateWaiterList(key string) *waiterList {
	val, _ := pa.waiters.LoadOrStore(key, &waiterList{})
	return val.(*waiterList)
}
