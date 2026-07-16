package gateway

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

type sessionDiagnostics struct {
	TerminationReason string
	ExitCode          int32
	StepCount         int
	DurationSeconds   int64
	Image             string
	ExperimentID      string
	Profile           string
}

func (g *Gateway) diagnoseSessionEnd(ctx context.Context, s *session, sessionID string) sessionDiagnostics {
	s.mu.RLock()
	allocation := s.runtimeAllocation()
	image := s.Info.Image
	profile := s.Info.Profile
	experimentID := s.experimentID
	createdAt := s.createdAt
	stepCount := 0
	if s.History != nil {
		stepCount = s.History.Len()
	}
	s.mu.RUnlock()

	diag := sessionDiagnostics{
		StepCount:       stepCount,
		DurationSeconds: int64(time.Since(createdAt).Seconds()),
		Image:           image,
		ExperimentID:    experimentID,
		Profile:         profile,
	}

	if g.k8sClient == nil || allocation.PodName == "" {
		return diag
	}

	namespace := allocation.Namespace
	if namespace == "" {
		namespace = g.runtimeNamespace()
	}

	pod := &corev1.Pod{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.PodName, Namespace: namespace}, pod); err != nil {
		return diag
	}

	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != "executor" {
			continue
		}
		if cs.LastTerminationState.Terminated != nil {
			diag.TerminationReason = cs.LastTerminationState.Terminated.Reason
			diag.ExitCode = cs.LastTerminationState.Terminated.ExitCode
		} else if cs.State.Terminated != nil {
			diag.TerminationReason = cs.State.Terminated.Reason
			diag.ExitCode = cs.State.Terminated.ExitCode
		}
		if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" && diag.TerminationReason == "" {
			if cs.LastTerminationState.Terminated != nil {
				diag.TerminationReason = cs.LastTerminationState.Terminated.Reason
				diag.ExitCode = cs.LastTerminationState.Terminated.ExitCode
			} else {
				diag.TerminationReason = "CrashLoopBackOff"
			}
		}
		break
	}

	return diag
}
