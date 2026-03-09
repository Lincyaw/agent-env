package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	"github.com/Lincyaw/agent-env/pkg/labels"
)

// PoolManagerConfig holds auto-scaling configuration for managed pools.
type PoolManagerConfig struct {
	// InitialReplicas is the starting replica count when a pool is first created.
	InitialReplicas int32
	// MinReplicas is the floor for scale-down.
	MinReplicas int32
	// MaxReplicas is the ceiling for scale-up.
	MaxReplicas int32
	// ScaleUpStep is how many replicas to add on each scale-up event.
	ScaleUpStep int32
	// IdleCooldown is how long a pool must have excess idle pods before scale-down.
	IdleCooldown time.Duration
	// EmptyPoolTTL is how long an empty pool (0 sessions) is kept before deletion.
	EmptyPoolTTL time.Duration
	// SweepInterval is how often the background goroutine checks for scale-down and GC.
	SweepInterval time.Duration
}

// DefaultPoolManagerConfig returns sensible defaults.
func DefaultPoolManagerConfig() PoolManagerConfig {
	return PoolManagerConfig{
		InitialReplicas: 2,
		MinReplicas:     0,
		MaxReplicas:     50,
		ScaleUpStep:     2,
		IdleCooldown:    5 * time.Minute,
		EmptyPoolTTL:    10 * time.Minute,
		SweepInterval:   30 * time.Second,
	}
}

// scaleReq is sent by AcquireSession callers into the per-pool coalesce channel.
// The coalesce goroutine performs a single K8s Update for all pending requests.
type scaleReq struct {
	done chan error
}

// poolState tracks per-pool metadata for managed pools.
type poolState struct {
	mu             sync.RWMutex
	image          string
	poolName       string
	namespace      string
	sessionCount   atomic.Int32
	lastSessionEnd time.Time
	idleSince      time.Time
	createdAt      time.Time
	resources      *corev1.ResourceRequirements
	tools          *arlv1alpha1.ToolsSpec
	workspaceDir   string
	scaleCh        chan scaleReq // per-pool coalesce channel for scale-up requests
	maxReplicas    atomic.Int32  // per-pool max replicas hint from clients (0 = use global config)
	poolCreated    bool          // true once the WarmPool CRD has been successfully created
}

// PoolManager manages WarmPools automatically for the managed session API.
type PoolManager struct {
	k8sClient      client.Client
	config         PoolManagerConfig
	pools          sync.Map // poolName → *poolState
	stopCh         chan struct{}
	wg             sync.WaitGroup
	coalesceWindow time.Duration // how long to batch scale-up requests before writing
}

// NewPoolManager creates a new PoolManager.
func NewPoolManager(k8sClient client.Client, config PoolManagerConfig) *PoolManager {
	return &PoolManager{
		k8sClient:      k8sClient,
		config:         config,
		stopCh:         make(chan struct{}),
		coalesceWindow: 50 * time.Millisecond,
	}
}

// Start launches the background sweep goroutine.
func (pm *PoolManager) Start() {
	pm.wg.Add(1)
	go pm.sweepLoop()
}

// Stop signals the sweep goroutine to exit and waits for it.
func (pm *PoolManager) Stop() {
	close(pm.stopCh)
	pm.wg.Wait()
}

// Recover rebuilds in-memory state from existing managed WarmPool CRDs on startup.
// Returns orphaned pods (allocated but no session) that should be released by the caller.
func (pm *PoolManager) Recover(ctx context.Context) ([]*corev1.Pod, error) {
	var orphanPods []*corev1.Pod

	var poolList arlv1alpha1.WarmPoolList
	if err := pm.k8sClient.List(ctx, &poolList,
		client.MatchingLabels{labelManaged: "true"}); err != nil {
		return nil, fmt.Errorf("list managed pools: %w", err)
	}

	for i := range poolList.Items {
		pool := &poolList.Items[i]
		image := pool.Annotations[annotationManagedImage]
		if image == "" {
			continue
		}

		state := &poolState{
			image:       image,
			poolName:    pool.Name,
			namespace:   pool.Namespace,
			createdAt:   pool.CreationTimestamp.Time,
			poolCreated: true, // recovered from existing CRD
		}

		// Restore resource/tools config from existing pool spec
		if len(pool.Spec.Template.Spec.Containers) > 0 {
			c := pool.Spec.Template.Spec.Containers[0]
			state.resources = &c.Resources
			if len(c.VolumeMounts) > 0 {
				state.workspaceDir = c.VolumeMounts[0].MountPath
			}
		}
		state.tools = pool.Spec.Tools

		// Count active sessions using pod labels as primary source.
		// Pod labels (StatusAllocated) are always present (written by PodAllocator).
		// On recovery, allocated pods are orphans (no session exists in the gateway).
		// We collect them so the caller can release them back to idle.
		var podList corev1.PodList
		if err := pm.k8sClient.List(ctx, &podList,
			client.InNamespace(pool.Namespace),
			client.MatchingLabels{
				labels.PoolLabelKey:   pool.Name,
				labels.StatusLabelKey: labels.StatusAllocated,
			}); err != nil {
			log.Printf("Warning: failed to list allocated pods for pool %s: %v", pool.Name, err)
		} else {
			poolOrphans := 0
			for j := range podList.Items {
				pod := &podList.Items[j]
				if pod.DeletionTimestamp == nil {
					orphanPods = append(orphanPods, pod.DeepCopy())
					poolOrphans++
				}
			}
			if poolOrphans > 0 {
				log.Printf("Recovered pool %s: %d orphaned allocated pods to release", pool.Name, poolOrphans)
			}
		}

		// Start with zero sessions — the gateway has no session entries yet.
		// Orphaned pods will be released by the caller.
		state.sessionCount.Store(0)
		state.lastSessionEnd = time.Now()

		pm.pools.Store(pool.Name, state)
		log.Printf("Recovered pool %s (image=%s, replicas=%d)", pool.Name, image, pool.Spec.Replicas)
	}

	return orphanPods, nil
}

const (
	labelManaged           = "arl.infra.io/managed"
	labelExperiment        = "arl.infra.io/experiment"
	annotationManagedImage = "arl.infra.io/managed-image"
)

// ErrPoolAtCapacity is returned when the pool has reached MaxReplicas
// and cannot accommodate more sessions.
var ErrPoolAtCapacity = errors.New("pool at maximum capacity")

// AcquireSession ensures a pool exists for the given image, scales up if needed,
// and returns the pool name for session creation.
func (pm *PoolManager) AcquireSession(ctx context.Context, req CreateManagedSessionRequest) (string, error) {
	ns := req.Namespace
	if ns == "" {
		ns = "default"
	}

	image := normalizeImage(req.Image)
	poolName := managedPoolName(image, ns)

	newState := &poolState{
		image:        image,
		poolName:     poolName,
		namespace:    ns,
		createdAt:    time.Now(),
		resources:    req.Resources,
		tools:        req.Tools,
		workspaceDir: req.WorkspaceDir,
	}

	actual, loaded := pm.pools.LoadOrStore(poolName, newState)
	state := actual.(*poolState)

	if !loaded {
		// First request for this image: create the WarmPool CRD.
		// On failure, mark as not-created so the next caller retries, but
		// do NOT delete from pools — other goroutines may already hold
		// a reference to this state via LoadOrStore.
		if err := pm.createPool(ctx, state, ns); err != nil {
			state.mu.Lock()
			state.poolCreated = false
			state.mu.Unlock()
			pm.pools.Delete(poolName)
			return "", fmt.Errorf("create managed pool: %w", err)
		}
		state.mu.Lock()
		state.poolCreated = true
		state.mu.Unlock()
		log.Printf("Created pool %s (image=%s, initReplicas=%d)", poolName, image, pm.config.InitialReplicas)
	} else {
		// Pool state exists, but the CRD might still be creating.
		// Wait briefly for pool creation to finish.
		if err := pm.waitPoolCreated(ctx, state); err != nil {
			return "", err
		}
	}

	// Increment BEFORE capacity check so concurrent calls see correct demand
	state.sessionCount.Add(1)

	// Update per-pool maxReplicas hint if the client provides a larger one.
	// This is a CAS loop so concurrent callers always keep the highest value.
	if req.MaxReplicas > 0 {
		for {
			cur := state.maxReplicas.Load()
			if req.MaxReplicas <= cur {
				break
			}
			if state.maxReplicas.CompareAndSwap(cur, req.MaxReplicas) {
				break
			}
		}
	}

	// Check if we need to scale up
	if err := pm.ensureCapacity(ctx, state); err != nil {
		state.sessionCount.Add(-1) // rollback on failure
		return "", fmt.Errorf("ensure pool capacity: %w", err)
	}

	// Fast-fail: if total sessions exceed MaxReplicas, this session can never
	// get a pod — the pool is at capacity and won't scale further.
	// We check AFTER ensureCapacity so the pool is already scaled to its max.
	if state.sessionCount.Load() > pm.config.MaxReplicas {
		state.sessionCount.Add(-1)
		return "", fmt.Errorf("%w: pool %s has %d/%d max replicas, %d sessions active",
			ErrPoolAtCapacity, state.poolName, pm.config.MaxReplicas, pm.config.MaxReplicas, state.sessionCount.Load()+1)
	}

	return poolName, nil
}

// waitPoolCreated waits for the pool CRD to be created by the first goroutine.
func (pm *PoolManager) waitPoolCreated(ctx context.Context, state *poolState) error {
	for {
		state.mu.RLock()
		created := state.poolCreated
		state.mu.RUnlock()
		if created {
			return nil
		}
		// Check if the pool state was removed (creation failed)
		if _, ok := pm.pools.Load(state.poolName); !ok {
			return fmt.Errorf("managed pool %s creation failed", state.poolName)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// ReleaseSession decrements the session count for a pool.
func (pm *PoolManager) ReleaseSession(poolName string) {
	val, ok := pm.pools.Load(poolName)
	if !ok {
		return
	}
	state := val.(*poolState)
	for {
		current := state.sessionCount.Load()
		if current <= 0 {
			return
		}
		if state.sessionCount.CompareAndSwap(current, current-1) {
			if current-1 <= 0 {
				state.mu.Lock()
				state.lastSessionEnd = time.Now()
				state.mu.Unlock()
			}
			return
		}
	}
}

// createPool creates a WarmPool CRD for a managed pool.
func (pm *PoolManager) createPool(ctx context.Context, state *poolState, ns string) error {
	resources := state.resources
	if resources == nil {
		resources = &corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		}
	}

	workspaceDir := state.workspaceDir
	if workspaceDir == "" {
		workspaceDir = "/workspace"
	}

	pool := &arlv1alpha1.WarmPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      state.poolName,
			Namespace: ns,
			Labels: map[string]string{
				labelManaged: "true",
			},
			Annotations: map[string]string{
				annotationManagedImage: state.image,
			},
		},
		Spec: arlv1alpha1.WarmPoolSpec{
			Replicas: pm.config.InitialReplicas,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            "executor",
							Image:           state.image,
							ImagePullPolicy: corev1.PullIfNotPresent,
							Command:         []string{"sh", "-c", "sleep infinity"},
							Resources:       *resources,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "workspace", MountPath: workspaceDir},
							},
						},
					},
				},
			},
			Tools: state.tools,
		},
	}

	if err := pm.k8sClient.Create(ctx, pool); err != nil {
		if apierrors.IsAlreadyExists(err) {
			// Another goroutine or previous run created it; that's fine
			return nil
		}
		return err
	}
	return nil
}

// ensureCapacity sends a scale-up request into the per-pool coalesce channel.
// A single goroutine per pool drains the channel, batches all pending requests
// within a short window, and performs one K8s Update for the batch.
// This eliminates optimistic concurrency conflicts under high concurrency.
func (pm *PoolManager) ensureCapacity(ctx context.Context, state *poolState) error {
	req := scaleReq{done: make(chan error, 1)}

	// Lazily start the coalesce goroutine for this pool
	state.mu.Lock()
	if state.scaleCh == nil {
		state.scaleCh = make(chan scaleReq, 1024)
		pm.wg.Add(1)
		go pm.scaleCoalesceLoop(state)
	}
	state.mu.Unlock()

	// Send the request (non-blocking: buffered channel)
	select {
	case state.scaleCh <- req:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Wait for the batch result
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// scaleCoalesceLoop is the single-writer goroutine for a pool.
// It drains pending scale-up requests, waits a short window to collect more,
// then performs one K8s read-modify-write (with RetryOnConflict) for the entire batch.
func (pm *PoolManager) scaleCoalesceLoop(state *poolState) {
	defer pm.wg.Done()

	for {
		// Block until at least one request arrives, or stop signal
		var first scaleReq
		var ok bool
		select {
		case first, ok = <-state.scaleCh:
			if !ok {
				return // channel closed
			}
		case <-pm.stopCh:
			return
		}

		// Collect more requests that arrive within the coalesce window
		batch := []scaleReq{first}
		timer := time.NewTimer(pm.coalesceWindow)
	drain:
		for {
			select {
			case req, ok := <-state.scaleCh:
				if !ok {
					timer.Stop()
					break drain
				}
				batch = append(batch, req)
			case <-timer.C:
				break drain
			case <-pm.stopCh:
				timer.Stop()
				// Notify all pending requests
				for _, r := range batch {
					r.done <- fmt.Errorf("pool manager stopped")
				}
				return
			}
		}

		// Perform a single K8s Update with RetryOnConflict for the entire batch
		err := pm.doScaleUp(state)

		// Fan-out the result to all callers in this batch
		for _, r := range batch {
			r.done <- err
		}
	}
}

// doScaleUp performs the actual K8s read-modify-write with RetryOnConflict.
func (pm *PoolManager) doScaleUp(state *poolState) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		pool := &arlv1alpha1.WarmPool{}
		if err := pm.k8sClient.Get(context.Background(), types.NamespacedName{
			Name: state.poolName, Namespace: state.namespace,
		}, pool); err != nil {
			return fmt.Errorf("get pool %s: %w", state.poolName, err)
		}

		// Calculate demand: sessionCount reflects all concurrent callers
		demand := state.sessionCount.Load() + 1 // +1 spare

		// If the client specified a maxReplicas hint, scale eagerly to that value.
		// This avoids incremental scale-ups when the client knows the final target.
		// NOTE: maxReplicas is an eager target, NOT a hard ceiling.
		// If actual demand exceeds maxReplicas, we still scale to meet demand.
		clientMax := state.maxReplicas.Load()
		if clientMax > 0 && clientMax > demand {
			demand = clientMax
		}

		desired := max(demand, pm.config.MinReplicas)
		desired = min(desired, pm.config.MaxReplicas)

		if pool.Spec.Replicas >= desired {
			return nil // Already at or above desired capacity
		}

		// Also ensure at least ScaleUpStep growth for responsiveness
		grown := pool.Spec.Replicas + pm.config.ScaleUpStep
		newReplicas := min(max(desired, grown), pm.config.MaxReplicas)
		if newReplicas <= pool.Spec.Replicas {
			return nil
		}

		oldReplicas := pool.Spec.Replicas
		pool.Spec.Replicas = newReplicas
		if err := pm.k8sClient.Update(context.Background(), pool); err != nil {
			// RetryOnConflict will re-read and retry on IsConflict errors
			return err
		}

		log.Printf("Scaled up pool %s (image=%s) %d→%d replicas (sessions=%d, demand=%d, clientMax=%d)",
			state.poolName, state.image, oldReplicas, newReplicas,
			state.sessionCount.Load(), demand, clientMax)
		return nil
	})
}

// sweepLoop runs periodically to scale down idle pools and garbage-collect empty pools.
func (pm *PoolManager) sweepLoop() {
	defer pm.wg.Done()
	ticker := time.NewTicker(pm.config.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.stopCh:
			return
		case <-ticker.C:
			pm.sweep()
		}
	}
}

// sweep performs one round of scale-down and GC.
func (pm *PoolManager) sweep() {
	pm.pools.Range(func(key, value any) bool {
		poolName := key.(string)
		state := value.(*poolState)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pool := &arlv1alpha1.WarmPool{}
		if err := pm.k8sClient.Get(ctx, types.NamespacedName{
			Name: poolName, Namespace: state.namespace,
		}, pool); err != nil {
			if apierrors.IsNotFound(err) {
				// Pool was deleted externally; clean up state
				pm.closeScaleCh(state)
				pm.pools.Delete(poolName)
			}
			return true
		}

		activeSessions := state.sessionCount.Load()

		// GC: delete pools with no sessions past TTL
		if activeSessions <= 0 {
			state.mu.RLock()
			lastEnd := state.lastSessionEnd
			state.mu.RUnlock()

			if !lastEnd.IsZero() && time.Since(lastEnd) > pm.config.EmptyPoolTTL {
				if err := pm.k8sClient.Delete(ctx, pool); err != nil && !apierrors.IsNotFound(err) {
					log.Printf("Warning: failed to GC managed pool %s: %v", poolName, err)
				} else {
					pm.closeScaleCh(state)
					pm.pools.Delete(poolName)
					log.Printf("GC'd empty pool %s (image=%s, idle for %v)", poolName, state.image, time.Since(lastEnd))
				}
				return true
			}
		}

		// Scale down: if we have more replicas than needed
		desiredReplicas := max(activeSessions+1, pm.config.MinReplicas) // keep 1 spare

		if pool.Spec.Replicas > desiredReplicas {
			state.mu.Lock()
			if state.idleSince.IsZero() {
				state.idleSince = time.Now()
				state.mu.Unlock()
				return true
			}
			elapsed := time.Since(state.idleSince)
			state.mu.Unlock()

			if elapsed > pm.config.IdleCooldown {
				scaleErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					fresh := &arlv1alpha1.WarmPool{}
					if err := pm.k8sClient.Get(ctx, types.NamespacedName{
						Name: poolName, Namespace: state.namespace,
					}, fresh); err != nil {
						return err
					}
					freshDesired := max(state.sessionCount.Load()+1, pm.config.MinReplicas)
					if fresh.Spec.Replicas <= freshDesired {
						return nil
					}
					fresh.Spec.Replicas = freshDesired
					return pm.k8sClient.Update(ctx, fresh)
				})
				if scaleErr != nil {
					log.Printf("Warning: failed to scale down managed pool %s: %v", poolName, scaleErr)
				} else {
					state.mu.Lock()
					state.idleSince = time.Time{}
					state.mu.Unlock()
					log.Printf("Scaled down pool %s (image=%s) to %d replicas (sessions=%d)", poolName, state.image, desiredReplicas, activeSessions)
				}
			}
		} else {
			// Not over-provisioned; reset idle tracking
			state.mu.Lock()
			state.idleSince = time.Time{}
			state.mu.Unlock()
		}

		return true
	})
}

// closeScaleCh closes the per-pool coalesce channel, causing scaleCoalesceLoop to exit.
func (pm *PoolManager) closeScaleCh(state *poolState) {
	state.mu.Lock()
	if state.scaleCh != nil {
		close(state.scaleCh)
		state.scaleCh = nil
	}
	state.mu.Unlock()
}

// DiagnosticStats returns the total pool count and per-pool session counts.
func (pm *PoolManager) DiagnosticStats() (poolCount int, perPool map[string]int32) {
	perPool = make(map[string]int32)
	pm.pools.Range(func(key, value any) bool {
		poolName := key.(string)
		state := value.(*poolState)
		perPool[poolName] = state.sessionCount.Load()
		poolCount++
		return true
	})
	return poolCount, perPool
}

// managedPoolName generates a deterministic pool name from image and namespace.
func managedPoolName(image, namespace string) string {
	h := sha256.Sum256([]byte(namespace + "/" + image))
	return "managed-" + hex.EncodeToString(h[:6])
}

// normalizeImage performs basic Docker image normalization.
func normalizeImage(image string) string {
	// Strip docker.io/library/ prefix
	image = strings.TrimPrefix(image, "docker.io/library/")
	image = strings.TrimPrefix(image, "docker.io/")

	// Add :latest tag if no tag specified
	if !strings.Contains(image, ":") && !strings.Contains(image, "@") {
		image = image + ":latest"
	}

	return image
}
