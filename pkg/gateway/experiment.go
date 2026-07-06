package gateway

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Lincyaw/agent-env/pkg/labels"
	"github.com/Lincyaw/agent-env/pkg/scheduling"
)

// CreateManagedSession creates a session with automatic pool management.
func (g *Gateway) CreateManagedSession(ctx context.Context, req CreateManagedSessionRequest) (*ManagedSessionInfo, error) {
	ctx, span := otel.Tracer("gateway").Start(ctx, "Gateway.CreateManagedSession",
		traceStartAttrs(
			"experiment.id", req.ExperimentID,
			"image", req.Image,
			"profile", req.Profile,
			"namespace", req.Namespace,
		),
	)
	defer span.End()

	if !validLabelValue.MatchString(req.ExperimentID) {
		err := fmt.Errorf("experimentId must be a valid Kubernetes label value (max 63 chars, alphanumeric/dash/underscore/dot, must start and end with alphanumeric)")
		recordSpanErr(span, err)
		return nil, err
	}

	ns, err := g.resolveNamespace(req.Namespace)
	if err != nil {
		recordSpanErr(span, err)
		return nil, err
	}

	if hasJSONPayload(req.Tools) {
		err := fmt.Errorf("managed session tools are not supported by sandbox-backed pools yet")
		recordSpanErr(span, err)
		return nil, err
	}
	if err := validatePrivateContainers(req.PrivateContainers); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	if _, err := parseConfigEnvVars(req.ConfigEnv); err != nil {
		recordSpanErr(span, err)
		return nil, err
	}
	image := normalizeImage(req.Image)
	profile := normalizeProfile(req.Profile)
	poolName, err := managedPoolName(image, ns, profile, req.PrivateContainers)
	if err != nil {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("derive managed pool name: %w", err)
	}
	createdPool := false
	if err := g.CreatePool(ctx, CreatePoolRequest{
		Name:              poolName,
		Image:             image,
		Profile:           profile,
		Replicas:          1,
		Namespace:         ns,
		Resources:         req.Resources,
		WorkspaceDir:      req.WorkspaceDir,
		PrivateContainers: req.PrivateContainers,
		Managed:           true,
	}); err != nil && !errors.IsAlreadyExists(err) {
		recordSpanErr(span, err)
		return nil, fmt.Errorf("ensure managed sandbox pool: %w", err)
	} else if err == nil {
		createdPool = true
	}
	span.SetAttributes(attribute.String("pool.name", poolName))

	info, err := g.CreateSession(ctx, CreateSessionRequest{
		Image:              image,
		Profile:            profile,
		PoolName:           poolName,
		Namespace:          ns,
		Mode:               req.Mode,
		Devbox:             req.Devbox,
		IdleTimeoutSeconds: req.IdleTimeoutSeconds,
		ConfigEnv:          req.ConfigEnv,
		Managed:            true,
		ExperimentID:       req.ExperimentID,
		PrivateContainers:  req.PrivateContainers,
	})
	if err != nil {
		if createdPool {
			if stopped, cleanupErr := g.stopManagedPoolIfUnused(ctx, poolName, ns); cleanupErr != nil {
				log.Printf("Warning: failed to cleanup unused managed pool %s/%s after managed session create failure: %v", ns, poolName, cleanupErr)
			} else if stopped {
				log.Printf("Stopped unused managed pool %s/%s after managed session create failure", ns, poolName)
			}
		}
		return nil, fmt.Errorf("create session: %w", err)
	}

	g.store.Sync(info.ID)

	return &ManagedSessionInfo{
		SessionInfo:  *info,
		ExperimentID: req.ExperimentID,
		Managed:      true,
	}, nil
}

// ListExperimentSessions returns all sessions for an experiment,
// including soft-deleted sessions whose history is still in Redis.
func (g *Gateway) ListExperimentSessions(experimentID string) []ManagedSessionInfo {
	results := make([]ManagedSessionInfo, 0)
	seen := make(map[string]bool)

	for _, id := range g.store.FindByExperiment(experimentID) {
		if s, ok := g.store.Get(id); ok {
			s.mu.RLock()
			if s.managed && s.experimentID == experimentID {
				results = append(results, ManagedSessionInfo{
					SessionInfo:  s.Info,
					ExperimentID: s.experimentID,
					Managed:      true,
				})
				seen[id] = true
			}
			s.mu.RUnlock()
			continue
		}
		if s, ok := g.store.GetHistorical(id); ok {
			s.mu.RLock()
			if s.managed && s.experimentID == experimentID {
				results = append(results, ManagedSessionInfo{
					SessionInfo:  s.Info,
					ExperimentID: s.experimentID,
					Managed:      true,
				})
				seen[id] = true
			}
			s.mu.RUnlock()
		}
	}
	if len(seen) > 0 {
		return results
	}

	g.store.Range(func(id string, s *session) bool {
		s.mu.RLock()
		if s.managed && s.experimentID == experimentID {
			results = append(results, ManagedSessionInfo{
				SessionInfo:  s.Info,
				ExperimentID: s.experimentID,
				Managed:      true,
			})
			seen[id] = true
		}
		s.mu.RUnlock()
		return true
	})

	for _, id := range g.store.FindByExperiment(experimentID) {
		if seen[id] {
			continue
		}
		if s, ok := g.store.GetHistorical(id); ok {
			s.mu.RLock()
			results = append(results, ManagedSessionInfo{
				SessionInfo:  s.Info,
				ExperimentID: s.experimentID,
				Managed:      true,
			})
			s.mu.RUnlock()
		}
	}

	return results
}

type indexedSessionStore interface {
	FindByExperiment(experimentID string) []string
	FindByProfile(profile string) []string
	FindByStatus(status string) []string
}

// ListSessions returns matching active sessions.
func (g *Gateway) ListSessions(options ...SessionListOptions) []SessionListItem {
	return g.ListSessionsPage(options...).Items
}

// ListSessionsPage returns a stable, optionally bounded page of active
// sessions. It uses store indexes when a selective filter is present.
func (g *Gateway) ListSessionsPage(options ...SessionListOptions) SessionListPage {
	var opts SessionListOptions
	if len(options) > 0 {
		opts = options[0]
	}
	if ids, ok := g.sessionCandidateIDs(opts); ok {
		return g.listSessionsFromCandidateIDs(ids, opts)
	}

	var items []SessionListItem
	g.store.Range(func(_ string, s *session) bool {
		if item, ok := sessionListItem(s, opts); ok {
			items = append(items, item)
		}
		return true
	})
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return pageSessionItems(items, opts)
}

func (g *Gateway) listSessionsFromCandidateIDs(ids []string, opts SessionListOptions) SessionListPage {
	sort.Strings(ids)
	cursor := strings.TrimSpace(opts.Cursor)
	limit := opts.Limit
	if limit < 0 {
		limit = 0
	}

	started := cursor == ""
	items := make([]SessionListItem, 0, minPositive(limit, len(ids)))
	nextCursor := ""
	lastID := ""
	for _, id := range ids {
		if id == lastID {
			continue
		}
		lastID = id
		if !started {
			if id == cursor {
				started = true
			}
			continue
		}
		s, found := g.store.Get(id)
		if !found {
			continue
		}
		if item, ok := sessionListItem(s, opts); ok {
			items = append(items, item)
		}
		if limit > 0 && len(items) >= limit {
			nextCursor = id
			break
		}
	}
	return SessionListPage{Items: items, NextCursor: nextCursor}
}

func minPositive(limit, total int) int {
	if limit > 0 && limit < total {
		return limit
	}
	return total
}

func (g *Gateway) sessionCandidateIDs(opts SessionListOptions) ([]string, bool) {
	indexed, ok := g.store.(indexedSessionStore)
	if !ok {
		return nil, false
	}
	if experimentID := strings.TrimSpace(opts.ExperimentID); experimentID != "" {
		return indexed.FindByExperiment(experimentID), true
	}
	if profile := strings.TrimSpace(opts.Profile); profile != "" {
		return indexed.FindByProfile(profile), true
	}
	if status := strings.TrimSpace(opts.Status); status != "" {
		return indexed.FindByStatus(status), true
	}
	return nil, false
}

func sessionListItem(s *session, opts SessionListOptions) (SessionListItem, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !sessionMatchesListOptionsLocked(s, opts) {
		return SessionListItem{}, false
	}
	return SessionListItem{
		SessionInfo:  s.Info,
		Managed:      s.managed,
		ExperimentID: s.experimentID,
	}, true
}

func sessionMatchesListOptions(s *session, opts SessionListOptions) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sessionMatchesListOptionsLocked(s, opts)
}

func sessionMatchesListOptionsLocked(s *session, opts SessionListOptions) bool {
	profile := strings.TrimSpace(opts.Profile)
	if profile != "" && s.Info.Profile != profile {
		return false
	}
	experimentID := strings.TrimSpace(opts.ExperimentID)
	if experimentID != "" && s.experimentID != experimentID {
		return false
	}
	status := strings.TrimSpace(opts.Status)
	if status != "" {
		sessionStatus := s.Info.Status
		if sessionStatus == "" {
			sessionStatus = "active"
		}
		if sessionStatus != status {
			return false
		}
	}
	return true
}

func pageSessionItems(items []SessionListItem, opts SessionListOptions) SessionListPage {
	cursor := strings.TrimSpace(opts.Cursor)
	limit := opts.Limit
	if limit < 0 {
		limit = 0
	}

	started := cursor == ""
	result := make([]SessionListItem, 0, len(items))
	for _, item := range items {
		if !started {
			if item.ID == cursor {
				started = true
			}
			continue
		}
		if limit > 0 && len(result) >= limit {
			return SessionListPage{Items: result, NextCursor: result[len(result)-1].ID}
		}
		result = append(result, item)
	}
	return SessionListPage{Items: result}
}

func (g *Gateway) Summary(ctx context.Context) (GatewaySummary, error) {
	summary := GatewaySummary{
		Sessions: g.store.Count(),
	}
	experiments := make(map[string]struct{})
	g.store.Range(func(_ string, s *session) bool {
		s.mu.RLock()
		if s.managed {
			summary.ManagedSessions++
		}
		if s.experimentID != "" {
			experiments[s.experimentID] = struct{}{}
		}
		s.mu.RUnlock()
		return true
	})
	summary.Experiments = len(experiments)

	pools, err := g.ListPoolsWithOptions(ctx, PoolListOptions{Namespace: g.runtimeNamespace()})
	if err != nil {
		return summary, err
	}
	summary.Pools = len(pools)
	for _, pool := range pools {
		summary.ReadyReplicas += pool.ReadyReplicas
		summary.AllocatedReplicas += pool.AllocatedReplicas
	}
	return summary, nil
}

// ListExperiments returns aggregate experiment summaries.
func (g *Gateway) ListExperiments() []ExperimentSummary {
	expMap := make(map[string]*ExperimentSummary)
	g.store.Range(func(_ string, s *session) bool {
		s.mu.RLock()
		if s.managed && s.experimentID != "" {
			if exp, ok := expMap[s.experimentID]; ok {
				exp.SessionCount++
			} else {
				expMap[s.experimentID] = &ExperimentSummary{
					ExperimentID: s.experimentID,
					SessionCount: 1,
					Image:        s.Info.Image,
					Profile:      s.Info.Profile,
					Namespace:    s.Info.Namespace,
				}
			}
		}
		s.mu.RUnlock()
		return true
	})

	results := make([]ExperimentSummary, 0, len(expMap))
	for _, v := range expMap {
		results = append(results, *v)
	}
	return results
}

// DeleteExperiment deletes all sessions for an experiment.
func (g *Gateway) DeleteExperiment(ctx context.Context, experimentID string) (int, error) {
	sessions := g.ListExperimentSessions(experimentID)
	pools := make(map[types.NamespacedName]struct{})
	for _, s := range sessions {
		if s.PoolRef == "" {
			continue
		}
		namespace := s.Namespace
		if namespace == "" {
			namespace = g.runtimeNamespace()
		}
		pools[types.NamespacedName{Name: s.PoolRef, Namespace: namespace}] = struct{}{}
	}

	deleted := 0
	var lastErr error
	for _, s := range sessions {
		if s.Status == "deleted" || s.DeletedAt != nil {
			continue
		}
		if err := g.DeleteSession(ctx, s.ID); err != nil {
			lastErr = err
			log.Printf("Warning: failed to delete session %s in experiment %s: %v", s.ID, experimentID, err)
		} else {
			deleted++
		}
	}

	for pool := range pools {
		if stoppedPool, err := g.stopManagedPoolIfUnused(ctx, pool.Name, pool.Namespace); err != nil {
			lastErr = err
			log.Printf("Warning: failed to stop unused managed pool %s/%s after deleting experiment %s: %v", pool.Namespace, pool.Name, experimentID, err)
		} else if stoppedPool {
			log.Printf("Stopped unused managed pool %s/%s after deleting experiment %s", pool.Namespace, pool.Name, experimentID)
		}
	}

	return deleted, lastErr
}

func (g *Gateway) stopManagedPoolIfUnused(ctx context.Context, poolName, namespace string) (bool, error) {
	if g.k8sClient == nil || strings.TrimSpace(poolName) == "" {
		return false, nil
	}
	namespace, err := g.resolveNamespace(namespace)
	if err != nil {
		return false, err
	}

	// Serialize check-then-stop: concurrent session deletes racing this path
	// could each observe "no bindings" while another is mid-decision. The pool
	// and binding state are re-read inside the critical section.
	g.poolStopMu.Lock()
	defer g.poolStopMu.Unlock()

	pool := &extensionsv1beta1.SandboxWarmPool{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: poolName, Namespace: namespace}, pool); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get managed pool %s/%s: %w", namespace, poolName, err)
	}
	if !isManagedPool(pool) {
		return false, nil
	}

	inUse, err := g.poolHasActiveBindings(ctx, poolName, namespace)
	if err != nil {
		return false, err
	}
	if inUse {
		return false, nil
	}
	if poolLifecycleStopped(pool) {
		return false, nil
	}

	if err := g.markPoolStopped(ctx, poolName, namespace); err != nil {
		return false, err
	}
	return true, nil
}

func poolLifecycleStopped(pool *extensionsv1beta1.SandboxWarmPool) bool {
	if pool == nil {
		return false
	}
	state := strings.ToLower(strings.TrimSpace(pool.Annotations[labels.PoolStateAnnotation]))
	autoscale := strings.ToLower(strings.TrimSpace(pool.Annotations[scheduling.PoolAutoscaleAnnotation]))
	return desiredSandboxWarmPoolReplicas(pool) == 0 && state == labels.PoolStateStopped && autoscale == "false"
}

func isManagedPool(pool *extensionsv1beta1.SandboxWarmPool) bool {
	if pool == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(pool.Annotations[labels.ManagedPoolAnnotation]), "true")
}

func (g *Gateway) poolHasActiveBindings(ctx context.Context, poolName, namespace string) (bool, error) {
	if g.store != nil {
		inUse := false
		g.store.Range(func(_ string, s *session) bool {
			s.mu.RLock()
			closed := s.closed
			allocation := s.runtimeAllocation()
			s.mu.RUnlock()
			if closed || allocation.PoolRef != poolName {
				return true
			}
			if allocation.Namespace != "" && allocation.Namespace != namespace {
				return true
			}
			inUse = true
			return false
		})
		if inUse {
			return true, nil
		}
	}

	var claims extensionsv1beta1.SandboxClaimList
	if err := g.k8sClient.List(ctx, &claims, client.InNamespace(namespace)); err != nil {
		return false, fmt.Errorf("list sandbox claims for managed pool cleanup: %w", err)
	}
	for i := range claims.Items {
		claim := &claims.Items[i]
		if claim.DeletionTimestamp != nil || claim.Spec.WarmPoolRef.Name != poolName {
			continue
		}
		return true, nil
	}
	return false, nil
}

// parseConfigEnvVars and validatePrivateContainers are declared in config_env.go and private_containers.go.
// normalizeImage and normalizeProfile are declared in managed_pool.go.
// hasJSONPayload is declared in types.go.
