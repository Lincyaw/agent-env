package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	configenvutil "github.com/Lincyaw/agent-env/pkg/configenv"
)

func TestSetConfigEnvFailureStatusPropagatesTopLevelConditions(t *testing.T) {
	pool := &arlv1alpha1.WarmPool{
		Status: arlv1alpha1.WarmPoolStatus{
			Conditions: []metav1.Condition{{
				Type:   "Ready",
				Status: metav1.ConditionTrue,
				Reason: "PoolReady",
			}},
			ConfigEnv: &arlv1alpha1.ConfigEnvStatus{
				Phase: arlv1alpha1.ConfigEnvPhaseReady,
				Conditions: []metav1.Condition{{
					Type:   "Ready",
					Status: metav1.ConditionTrue,
					Reason: "ConfigEnvReady",
				}},
			},
		},
	}

	r := &WarmPoolReconciler{}
	r.setConfigEnvFailureStatus(pool, "missing template var")

	if pool.Status.ConfigEnv == nil {
		t.Fatal("ConfigEnv status was not set")
	}
	if pool.Status.ConfigEnv.Phase != arlv1alpha1.ConfigEnvPhaseFailed {
		t.Fatalf("ConfigEnv phase = %q, want %q", pool.Status.ConfigEnv.Phase, arlv1alpha1.ConfigEnvPhaseFailed)
	}

	configCond := findCondition(pool.Status.Conditions, configenvutil.ReadyConditionType)
	if configCond == nil {
		t.Fatal("missing top-level ConfigEnvReady condition")
	}
	if configCond.Status != metav1.ConditionFalse {
		t.Fatalf("ConfigEnvReady status = %q, want %q", configCond.Status, metav1.ConditionFalse)
	}
	if configCond.Reason != "ConfigEnvFailed" {
		t.Fatalf("ConfigEnvReady reason = %q, want %q", configCond.Reason, "ConfigEnvFailed")
	}

	readyCond := findCondition(pool.Status.Conditions, "Ready")
	if readyCond == nil {
		t.Fatal("missing top-level Ready condition")
	}
	if readyCond.Status != metav1.ConditionFalse {
		t.Fatalf("Ready status = %q, want %q", readyCond.Status, metav1.ConditionFalse)
	}
	if readyCond.Reason != "ConfigEnvFailed" {
		t.Fatalf("Ready reason = %q, want %q", readyCond.Reason, "ConfigEnvFailed")
	}
}
