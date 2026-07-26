package gateway

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

// The sandbox NetworkPolicy has to describe the real data path. If it does not,
// a cluster that actually enforces policy cuts the gateway off from every
// sandbox — which is why enforcement could never be switched on.

func TestSandboxPolicyAdmitsTheGatewayOnTheExecutorPort(t *testing.T) {
	policy := denyInternetEgressPolicyWithPort([]string{"10.0.0.0/8"}, 9090)

	if len(policy.Ingress) == 0 {
		t.Fatal("policy has no ingress rules; the controller default would lock out the gateway")
	}

	var gatewayRule bool
	for _, rule := range policy.Ingress {
		for _, from := range rule.From {
			if from.PodSelector == nil {
				continue
			}
			labels := from.PodSelector.MatchLabels
			if labels["app.kubernetes.io/component"] != "gateway" {
				continue
			}
			if labels["app.kubernetes.io/name"] != "agent-env" {
				t.Errorf("gateway selector missing name label: %v", labels)
			}
			gatewayRule = true
			if len(rule.Ports) != 1 {
				t.Fatalf("expected the rule scoped to one port, got %d", len(rule.Ports))
			}
			if got := rule.Ports[0].Port.IntValue(); got != 9090 {
				t.Errorf("gateway ingress port = %d, want 9090", got)
			}
			if p := rule.Ports[0].Protocol; p == nil || *p != corev1.ProtocolTCP {
				t.Errorf("gateway ingress protocol = %v, want TCP", p)
			}
		}
	}
	if !gatewayRule {
		t.Error("no ingress rule admits the gateway")
	}
}

func TestSandboxPolicyKeepsTheControllerRouterPath(t *testing.T) {
	policy := denyInternetEgressPolicyWithPort([]string{"10.0.0.0/8"}, 9090)

	for _, rule := range policy.Ingress {
		for _, from := range rule.From {
			if from.NamespaceSelector == nil || from.PodSelector == nil {
				continue
			}
			ns := from.NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"]
			if ns == "agent-sandbox-system" &&
				from.PodSelector.MatchLabels["app"] == "sandbox-router" {
				return
			}
		}
	}
	t.Error("policy drops the sandbox-router ingress the controller relies on")
}

func TestSandboxPolicyIngressFollowsTheConfiguredPort(t *testing.T) {
	policy := denyInternetEgressPolicyWithPort(nil, 7777)

	for _, rule := range policy.Ingress {
		for _, from := range rule.From {
			if from.PodSelector != nil &&
				from.PodSelector.MatchLabels["app.kubernetes.io/component"] == "gateway" {
				if got := rule.Ports[0].Port.IntValue(); got != 7777 {
					t.Errorf("ingress port = %d, want the configured 7777", got)
				}
				return
			}
		}
	}
	t.Error("gateway rule not found")
}

func TestSandboxPolicyStillDeniesInternetEgress(t *testing.T) {
	policy := denyInternetEgressPolicyWithPort([]string{"10.0.0.0/8", "172.16.0.0/12"}, 9090)

	var cidrs []string
	for _, rule := range policy.Egress {
		for _, to := range rule.To {
			if to.IPBlock != nil {
				cidrs = append(cidrs, to.IPBlock.CIDR)
			}
		}
	}
	if len(cidrs) != 2 {
		t.Fatalf("egress allow-list = %v, want the two configured CIDRs", cidrs)
	}
	// Adding ingress must not have opened egress to everything.
	for _, c := range cidrs {
		if c == "0.0.0.0/0" {
			t.Error("egress allows the whole internet")
		}
	}
}

func TestExecutorPortOrDefault(t *testing.T) {
	if got := (&Gateway{}).executorPortOrDefault(); got != 9090 {
		t.Errorf("default = %d, want 9090", got)
	}
	gw := &Gateway{gwConfig: GatewayConfig{ExecutorPort: 7777}}
	if got := gw.executorPortOrDefault(); got != 7777 {
		t.Errorf("configured = %d, want 7777", got)
	}
}

func TestAllowInternetPolicyAdmitsGatewayAndKeepsEgressPosture(t *testing.T) {
	policy := allowInternetEgressPolicy(9090)

	var gatewayRule bool
	for _, rule := range policy.Ingress {
		for _, from := range rule.From {
			if from.PodSelector != nil &&
				from.PodSelector.MatchLabels["app.kubernetes.io/component"] == "gateway" {
				gatewayRule = true
			}
		}
	}
	if !gatewayRule {
		t.Error("managed default must admit the gateway or enforcement severs the data path")
	}

	// Egress must stay as it was: internet yes, cluster no.
	if len(policy.Egress) != 1 || len(policy.Egress[0].To) != 1 {
		t.Fatalf("unexpected egress shape: %+v", policy.Egress)
	}
	block := policy.Egress[0].To[0].IPBlock
	if block == nil || block.CIDR != "0.0.0.0/0" {
		t.Fatalf("expected internet egress, got %+v", block)
	}
	want := map[string]bool{
		"10.0.0.0/8": false, "172.16.0.0/12": false,
		"192.168.0.0/16": false, "169.254.0.0/16": false,
	}
	for _, e := range block.Except {
		if _, ok := want[e]; !ok {
			t.Errorf("unexpected except CIDR %q", e)
		}
		want[e] = true
	}
	for cidr, seen := range want {
		if !seen {
			t.Errorf("private range %s is no longer excluded from egress", cidr)
		}
	}
}
