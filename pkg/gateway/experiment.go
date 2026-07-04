package gateway

import (
	"context"
	"fmt"
	"log"
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
		MaxLifetimeSeconds: req.MaxLifetimeSeconds,
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

// ListSessions returns all active sessions.
func (g *Gateway) ListSessions() []SessionListItem {
	var items []SessionListItem
	g.store.Range(func(_ string, s *session) bool {
		s.mu.RLock()
		item := SessionListItem{
			SessionInfo:  s.Info,
			Managed:      s.managed,
			ExperimentID: s.experimentID,
		}
		s.mu.RUnlock()
		items = append(items, item)
		return true
	})
	return items
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
