package gateway

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	toolscache "k8s.io/client-go/tools/cache"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

// PoolReadModel is the gateway hot-path view of Kubernetes pool state.
// Implementations must return compact summaries without issuing Kubernetes list
// calls for every API request once Synced is true.
type PoolReadModel interface {
	Synced() bool
	ListPools(opts PoolListOptions) []PoolInfo
	SnapshotPools(namespace string) []PoolSnapshot
	SnapshotPoolsForIntent(intent ResourceIntent) []PoolSnapshot
	SnapshotPool(namespace, poolName string) (PoolSnapshot, bool)
}

type poolIndex struct {
	mu          sync.RWMutex
	synced      bool
	pools       map[types.NamespacedName]indexedPool
	templates   map[types.NamespacedName]indexedTemplate
	claims      map[types.NamespacedName]types.NamespacedName
	claimCounts map[types.NamespacedName]int32
}

type indexedPool struct {
	Name          string
	Namespace     string
	TemplateName  string
	Profile       string
	State         string
	Replicas      int32
	ReadyReplicas int32
	CreatedAt     time.Time
}

type indexedTemplate struct {
	Name      string
	Namespace string
	Profile   string
	Image     string
}

func newPoolIndex() *poolIndex {
	return &poolIndex{
		pools:       make(map[types.NamespacedName]indexedPool),
		templates:   make(map[types.NamespacedName]indexedTemplate),
		claims:      make(map[types.NamespacedName]types.NamespacedName),
		claimCounts: make(map[types.NamespacedName]int32),
	}
}

func (idx *poolIndex) Synced() bool {
	if idx == nil {
		return false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.synced
}

func (idx *poolIndex) setSynced(synced bool) {
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.synced = synced
}

func (idx *poolIndex) replace(pools []indexedPool, templates []indexedTemplate, claims map[types.NamespacedName]types.NamespacedName) {
	nextPools := make(map[types.NamespacedName]indexedPool, len(pools))
	for _, pool := range pools {
		nextPools[types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}] = pool
	}
	nextTemplates := make(map[types.NamespacedName]indexedTemplate, len(templates))
	for _, template := range templates {
		nextTemplates[types.NamespacedName{Name: template.Name, Namespace: template.Namespace}] = template
	}
	nextClaims := make(map[types.NamespacedName]types.NamespacedName, len(claims))
	nextClaimCounts := make(map[types.NamespacedName]int32)
	for claimKey, poolKey := range claims {
		nextClaims[claimKey] = poolKey
		nextClaimCounts[poolKey]++
	}

	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pools = nextPools
	idx.templates = nextTemplates
	idx.claims = nextClaims
	idx.claimCounts = nextClaimCounts
	idx.synced = true
}

func (idx *poolIndex) upsertPool(pool *extensionsv1beta1.SandboxWarmPool) {
	if idx == nil || pool == nil {
		return
	}
	indexed := indexedPoolFromObject(pool)
	key := types.NamespacedName{Name: indexed.Name, Namespace: indexed.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.pools[key] = indexed
}

func (idx *poolIndex) deletePool(pool *extensionsv1beta1.SandboxWarmPool) {
	if idx == nil || pool == nil {
		return
	}
	key := types.NamespacedName{Name: pool.Name, Namespace: pool.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.pools, key)
	delete(idx.claimCounts, key)
	for claimKey, poolKey := range idx.claims {
		if poolKey == key {
			delete(idx.claims, claimKey)
		}
	}
}

func (idx *poolIndex) upsertTemplate(template *extensionsv1beta1.SandboxTemplate) {
	if idx == nil || template == nil {
		return
	}
	indexed := indexedTemplateFromObject(template)
	key := types.NamespacedName{Name: indexed.Name, Namespace: indexed.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	idx.templates[key] = indexed
}

func (idx *poolIndex) deleteTemplate(template *extensionsv1beta1.SandboxTemplate) {
	if idx == nil || template == nil {
		return
	}
	key := types.NamespacedName{Name: template.Name, Namespace: template.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	delete(idx.templates, key)
}

func (idx *poolIndex) upsertClaim(claim *extensionsv1beta1.SandboxClaim) {
	if idx == nil || claim == nil {
		return
	}
	if claim.DeletionTimestamp != nil {
		idx.deleteClaim(claim)
		return
	}
	poolName := strings.TrimSpace(claim.Spec.WarmPoolRef.Name)
	if poolName == "" {
		return
	}
	claimKey := types.NamespacedName{Name: claim.Name, Namespace: claim.Namespace}
	poolKey := types.NamespacedName{Name: poolName, Namespace: claim.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if oldPoolKey, ok := idx.claims[claimKey]; ok {
		if oldPoolKey == poolKey {
			return
		}
		if idx.claimCounts[oldPoolKey] > 0 {
			idx.claimCounts[oldPoolKey]--
		}
	}
	idx.claims[claimKey] = poolKey
	idx.claimCounts[poolKey]++
}

func (idx *poolIndex) deleteClaim(claim *extensionsv1beta1.SandboxClaim) {
	if idx == nil || claim == nil {
		return
	}
	claimKey := types.NamespacedName{Name: claim.Name, Namespace: claim.Namespace}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	poolKey, ok := idx.claims[claimKey]
	if !ok {
		poolName := strings.TrimSpace(claim.Spec.WarmPoolRef.Name)
		if poolName == "" {
			return
		}
		poolKey = types.NamespacedName{Name: poolName, Namespace: claim.Namespace}
	}
	delete(idx.claims, claimKey)
	if idx.claimCounts[poolKey] > 0 {
		idx.claimCounts[poolKey]--
	}
}

func (idx *poolIndex) ListPools(opts PoolListOptions) []PoolInfo {
	if idx == nil {
		return nil
	}
	namespace := strings.TrimSpace(opts.Namespace)
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	capHint := len(idx.pools)
	if !opts.IncludeStopped && capHint > 64 {
		capHint = 64
	}
	pools := make([]PoolInfo, 0, capHint)
	for key, pool := range idx.pools {
		if namespace != "" && key.Namespace != namespace {
			continue
		}
		allocated := idx.claimCounts[key]
		if !opts.IncludeStopped && indexedPoolStopped(pool, allocated) {
			continue
		}
		template := idx.templates[types.NamespacedName{Name: pool.TemplateName, Namespace: pool.Namespace}]
		pools = append(pools, PoolInfo{
			Name:              pool.Name,
			Namespace:         pool.Namespace,
			Profile:           firstNonEmpty(pool.Profile, template.Profile, defaultPoolProfile),
			Image:             template.Image,
			Replicas:          pool.Replicas,
			ReadyReplicas:     pool.ReadyReplicas,
			AllocatedReplicas: allocated,
			State:             firstNonEmpty(pool.State, labels.PoolStateRunning),
			CreatedAt:         pool.CreatedAt,
		})
	}
	sort.SliceStable(pools, func(i, j int) bool {
		if pools[i].CreatedAt.Equal(pools[j].CreatedAt) {
			return pools[i].Name < pools[j].Name
		}
		return pools[i].CreatedAt.Before(pools[j].CreatedAt)
	})
	return pools
}

func (idx *poolIndex) SnapshotPools(namespace string) []PoolSnapshot {
	if idx == nil {
		return nil
	}
	namespace = strings.TrimSpace(namespace)
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	snapshots := make([]PoolSnapshot, 0, len(idx.pools))
	for key, pool := range idx.pools {
		if namespace != "" && key.Namespace != namespace {
			continue
		}
		snapshots = append(snapshots, idx.snapshotLocked(key, pool))
	}
	sortPoolSnapshots(snapshots)
	return snapshots
}

func (idx *poolIndex) SnapshotPoolsForIntent(intent ResourceIntent) []PoolSnapshot {
	if idx == nil {
		return nil
	}
	scope := intent.Scope.normalized()
	profile := strings.TrimSpace(intent.Profile)
	image := strings.TrimSpace(intent.Image)

	idx.mu.RLock()
	defer idx.mu.RUnlock()

	active := make([]PoolSnapshot, 0, 16)
	for key, pool := range idx.pools {
		if key.Namespace != scope.Namespace {
			continue
		}
		snapshot := idx.snapshotLocked(key, pool)
		if profile != "" && snapshot.Profile != profile {
			continue
		}
		if image != "" && snapshot.Image != image {
			continue
		}
		if indexedPoolStopped(pool, idx.claimCounts[key]) {
			continue
		}
		active = append(active, snapshot)
	}
	if len(active) > 0 {
		sortPoolSnapshots(active)
		return active
	}

	stopped := make([]PoolSnapshot, 0)
	for key, pool := range idx.pools {
		if key.Namespace != scope.Namespace {
			continue
		}
		if !indexedPoolStopped(pool, idx.claimCounts[key]) {
			continue
		}
		snapshot := idx.snapshotLocked(key, pool)
		if profile != "" && snapshot.Profile != profile {
			continue
		}
		if image != "" && snapshot.Image != image {
			continue
		}
		stopped = append(stopped, snapshot)
	}
	sortPoolSnapshots(stopped)
	return stopped
}

func (idx *poolIndex) SnapshotPool(namespace, poolName string) (PoolSnapshot, bool) {
	if idx == nil {
		return PoolSnapshot{}, false
	}
	key := types.NamespacedName{Name: poolName, Namespace: namespace}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	pool, ok := idx.pools[key]
	if !ok {
		return PoolSnapshot{}, false
	}
	return idx.snapshotLocked(key, pool), true
}

func sortPoolSnapshots(snapshots []PoolSnapshot) {
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].Name < snapshots[j].Name
	})
}

func (idx *poolIndex) snapshotLocked(key types.NamespacedName, pool indexedPool) PoolSnapshot {
	template := idx.templates[types.NamespacedName{Name: pool.TemplateName, Namespace: pool.Namespace}]
	return PoolSnapshot{
		Name:              pool.Name,
		Namespace:         pool.Namespace,
		Profile:           firstNonEmpty(pool.Profile, template.Profile, defaultPoolProfile),
		Image:             template.Image,
		DesiredReplicas:   pool.Replicas,
		ReadyReplicas:     pool.ReadyReplicas,
		AllocatedReplicas: idx.claimCounts[key],
	}
}

func (g *Gateway) StartPoolIndex(ctx context.Context, scheme *runtime.Scheme) error {
	if g.k8sRESTConfig == nil {
		return nil
	}
	idx := g.ensurePoolIndex()
	namespace := g.runtimeNamespace()
	poolCache, err := ctrlcache.New(g.k8sRESTConfig, ctrlcache.Options{
		Scheme: scheme,
		DefaultNamespaces: map[string]ctrlcache.Config{
			namespace: {},
		},
		DefaultTransform: ctrlcache.TransformStripManagedFields(),
	})
	if err != nil {
		return fmt.Errorf("create pool index cache: %w", err)
	}
	if err := idx.addInformerHandlers(ctx, poolCache); err != nil {
		return err
	}
	go func() {
		if err := poolCache.Start(ctx); err != nil && ctx.Err() == nil {
			log.Printf("Warning: pool index cache stopped: %v", err)
		}
	}()
	if !poolCache.WaitForCacheSync(ctx) {
		return fmt.Errorf("pool index cache sync failed")
	}
	if err := g.refreshPoolIndexFromReader(ctx, poolCache); err != nil {
		return err
	}
	return nil
}

func (g *Gateway) refreshPoolIndexFromReader(ctx context.Context, reader client.Reader) error {
	idx := g.ensurePoolIndex()
	namespace := g.runtimeNamespace()

	var poolList extensionsv1beta1.SandboxWarmPoolList
	if err := reader.List(ctx, &poolList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox warm pools for pool index: %w", err)
	}
	pools := make([]indexedPool, 0, len(poolList.Items))
	for i := range poolList.Items {
		pools = append(pools, indexedPoolFromObject(&poolList.Items[i]))
	}

	var templateList extensionsv1beta1.SandboxTemplateList
	if err := reader.List(ctx, &templateList, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox templates for pool index: %w", err)
	}
	templates := make([]indexedTemplate, 0, len(templateList.Items))
	for i := range templateList.Items {
		templates = append(templates, indexedTemplateFromObject(&templateList.Items[i]))
	}

	var claims extensionsv1beta1.SandboxClaimList
	if err := reader.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return fmt.Errorf("list sandbox claims for pool index: %w", err)
	}
	claimsByPool := make(map[types.NamespacedName]types.NamespacedName)
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name == "" {
			continue
		}
		claimKey := types.NamespacedName{Name: claim.Name, Namespace: claim.Namespace}
		poolKey := types.NamespacedName{Name: claim.Spec.WarmPoolRef.Name, Namespace: claim.Namespace}
		claimsByPool[claimKey] = poolKey
	}

	idx.replace(pools, templates, claimsByPool)
	return nil
}

func (idx *poolIndex) addInformerHandlers(ctx context.Context, poolCache ctrlcache.Cache) error {
	poolInformer, err := poolCache.GetInformer(ctx, &extensionsv1beta1.SandboxWarmPool{})
	if err != nil {
		return fmt.Errorf("get sandbox warm pool informer: %w", err)
	}
	if _, err := poolInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if pool, ok := sandboxWarmPoolFromEventObject(obj); ok {
				idx.upsertPool(pool)
			}
		},
		UpdateFunc: func(_, obj any) {
			if pool, ok := sandboxWarmPoolFromEventObject(obj); ok {
				idx.upsertPool(pool)
			}
		},
		DeleteFunc: func(obj any) {
			if pool, ok := sandboxWarmPoolFromEventObject(obj); ok {
				idx.deletePool(pool)
			}
		},
	}); err != nil {
		return fmt.Errorf("add sandbox warm pool index handler: %w", err)
	}

	templateInformer, err := poolCache.GetInformer(ctx, &extensionsv1beta1.SandboxTemplate{})
	if err != nil {
		return fmt.Errorf("get sandbox template informer: %w", err)
	}
	if _, err := templateInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if template, ok := sandboxTemplateFromEventObject(obj); ok {
				idx.upsertTemplate(template)
			}
		},
		UpdateFunc: func(_, obj any) {
			if template, ok := sandboxTemplateFromEventObject(obj); ok {
				idx.upsertTemplate(template)
			}
		},
		DeleteFunc: func(obj any) {
			if template, ok := sandboxTemplateFromEventObject(obj); ok {
				idx.deleteTemplate(template)
			}
		},
	}); err != nil {
		return fmt.Errorf("add sandbox template index handler: %w", err)
	}

	claimInformer, err := poolCache.GetInformer(ctx, &extensionsv1beta1.SandboxClaim{})
	if err != nil {
		return fmt.Errorf("get sandbox claim informer: %w", err)
	}
	if _, err := claimInformer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: func(obj any) {
			if claim, ok := sandboxClaimFromEventObject(obj); ok {
				idx.upsertClaim(claim)
			}
		},
		UpdateFunc: func(_, obj any) {
			if claim, ok := sandboxClaimFromEventObject(obj); ok {
				idx.upsertClaim(claim)
			}
		},
		DeleteFunc: func(obj any) {
			if claim, ok := sandboxClaimFromEventObject(obj); ok {
				idx.deleteClaim(claim)
			}
		},
	}); err != nil {
		return fmt.Errorf("add sandbox claim index handler: %w", err)
	}
	return nil
}

func (g *Gateway) ensurePoolIndex() *poolIndex {
	g.poolIndexMu.Lock()
	defer g.poolIndexMu.Unlock()
	if g.poolIndex == nil {
		g.poolIndex = newPoolIndex()
	}
	if g.poolReadModel == nil {
		g.poolReadModel = g.poolIndex
	}
	return g.poolIndex
}

func (g *Gateway) syncedPoolReadModel() (PoolReadModel, bool) {
	if g == nil || g.poolReadModel == nil || !g.poolReadModel.Synced() {
		return nil, false
	}
	return g.poolReadModel, true
}

func indexedPoolFromObject(pool *extensionsv1beta1.SandboxWarmPool) indexedPool {
	return indexedPool{
		Name:          pool.Name,
		Namespace:     pool.Namespace,
		TemplateName:  pool.Spec.TemplateRef.Name,
		Profile:       profileFromObjectMeta(pool.ObjectMeta),
		State:         firstNonEmpty(pool.Annotations[labels.PoolStateAnnotation], pool.Labels[labels.PoolStateLabelKey], labels.PoolStateRunning),
		Replicas:      desiredSandboxWarmPoolReplicas(pool),
		ReadyReplicas: pool.Status.ReadyReplicas,
		CreatedAt:     pool.CreationTimestamp.Time,
	}
}

func indexedTemplateFromObject(template *extensionsv1beta1.SandboxTemplate) indexedTemplate {
	return indexedTemplate{
		Name:      template.Name,
		Namespace: template.Namespace,
		Profile:   profileFromObjectMeta(template.ObjectMeta),
		Image:     primarySandboxTemplateImage(template),
	}
}

func indexedPoolStopped(pool indexedPool, allocated int32) bool {
	if allocated > 0 {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(pool.State))
	if state == labels.PoolStateStopped {
		return true
	}
	return pool.Replicas == 0 && pool.ReadyReplicas == 0
}

func sandboxWarmPoolFromEventObject(obj any) (*extensionsv1beta1.SandboxWarmPool, bool) {
	switch typed := obj.(type) {
	case *extensionsv1beta1.SandboxWarmPool:
		return typed, true
	case toolscache.DeletedFinalStateUnknown:
		pool, ok := typed.Obj.(*extensionsv1beta1.SandboxWarmPool)
		return pool, ok
	default:
		return nil, false
	}
}

func sandboxTemplateFromEventObject(obj any) (*extensionsv1beta1.SandboxTemplate, bool) {
	switch typed := obj.(type) {
	case *extensionsv1beta1.SandboxTemplate:
		return typed, true
	case toolscache.DeletedFinalStateUnknown:
		template, ok := typed.Obj.(*extensionsv1beta1.SandboxTemplate)
		return template, ok
	default:
		return nil, false
	}
}

func sandboxClaimFromEventObject(obj any) (*extensionsv1beta1.SandboxClaim, bool) {
	switch typed := obj.(type) {
	case *extensionsv1beta1.SandboxClaim:
		return typed, true
	case toolscache.DeletedFinalStateUnknown:
		claim, ok := typed.Obj.(*extensionsv1beta1.SandboxClaim)
		return claim, ok
	default:
		return nil, false
	}
}
