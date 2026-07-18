package gateway

import (
	"context"
	"fmt"
	"log"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"

	"github.com/Lincyaw/agent-env/pkg/labels"
)

// sessionNetworkPolicyName returns the Kubernetes NetworkPolicy name for a
// per-session network policy override.
func sessionNetworkPolicyName(sessionID string) string {
	return dnsLabelWithSuffix(sessionID, "-netpol")
}

// UpdateSessionNetworkPolicy creates, updates, or deletes the per-session
// Kubernetes NetworkPolicy to dynamically toggle internet access for an
// active session's pod. This is orthogonal to the template-level shared
// NetworkPolicy managed by the sandbox controller: K8s NetworkPolicies are
// additive, so a per-session "allow-all-egress" policy overrides a
// template-level deny, while a per-session "deny-internet" policy is
// effective even when the template has no restrictions.
func (g *Gateway) UpdateSessionNetworkPolicy(ctx context.Context, sessionID string, req UpdateNetworkPolicyRequest) error {
	if g.sandboxNetworkPolicyManagement() != extensionsv1beta1.NetworkPolicyManagementManaged {
		return fmt.Errorf("network policy updates require SANDBOX_NETWORK_POLICY_MANAGEMENT=Managed (current: %s)",
			g.gwConfig.SandboxNetworkPolicyManagement)
	}
	if g.k8sClient == nil {
		return fmt.Errorf("network policy updates require a Kubernetes client")
	}

	s, ok := g.store.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	s.mu.RLock()
	allocation := s.runtimeAllocation()
	s.mu.RUnlock()

	if allocation.ClaimName == "" || allocation.Namespace == "" {
		return fmt.Errorf("session %s has no sandbox claim binding", sessionID)
	}

	claim := &extensionsv1beta1.SandboxClaim{}
	if err := g.k8sClient.Get(ctx, types.NamespacedName{Name: allocation.ClaimName, Namespace: allocation.Namespace}, claim); err != nil {
		return fmt.Errorf("get sandbox claim %s/%s: %w", allocation.Namespace, allocation.ClaimName, err)
	}
	claimUID := string(claim.UID)
	if claimUID == "" {
		return fmt.Errorf("sandbox claim %s/%s has no UID", allocation.Namespace, allocation.ClaimName)
	}

	npName := sessionNetworkPolicyName(sessionID)
	ns := allocation.Namespace

	if req.AllowInternet != nil && *req.AllowInternet {
		return g.applySessionNetworkPolicy(ctx, ns, npName, claimUID, sessionID, allowAllEgressRules())
	}

	cidrs := req.EgressCIDRs
	if len(cidrs) == 0 {
		cidrs = g.egressAllowCIDRs()
	}
	return g.applySessionNetworkPolicy(ctx, ns, npName, claimUID, sessionID, denyInternetEgressRules(cidrs))
}

// applySessionNetworkPolicy creates or updates a per-session NetworkPolicy.
func (g *Gateway) applySessionNetworkPolicy(
	ctx context.Context,
	namespace, name, claimUID, sessionID string,
	egress []networkingv1.NetworkPolicyEgressRule,
) error {
	desired := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				labels.SessionAnnotation: sessionID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					extensionsv1beta1.SandboxIDLabel: claimUID,
				},
			},
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      egress,
		},
	}

	existing := &networkingv1.NetworkPolicy{}
	err := g.k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get network policy %s/%s: %w", namespace, name, err)
		}
		if err := g.k8sClient.Create(ctx, desired); err != nil {
			return fmt.Errorf("create network policy %s/%s: %w", namespace, name, err)
		}
		log.Printf("Created per-session network policy %s/%s for session %s", namespace, name, sessionID)
		return nil
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
	if err := g.k8sClient.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("update network policy %s/%s: %w", namespace, name, err)
	}
	log.Printf("Updated per-session network policy %s/%s for session %s", namespace, name, sessionID)
	return nil
}

// DeleteSessionNetworkPolicy removes the per-session NetworkPolicy for a
// session. It is safe to call when no policy exists.
func (g *Gateway) DeleteSessionNetworkPolicy(ctx context.Context, sessionID, namespace string) {
	if g.k8sClient == nil {
		return
	}
	if g.sandboxNetworkPolicyManagement() != extensionsv1beta1.NetworkPolicyManagementManaged {
		return
	}
	npName := sessionNetworkPolicyName(sessionID)
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: namespace,
		},
	}
	if err := g.k8sClient.Delete(ctx, np); err != nil && !apierrors.IsNotFound(err) {
		log.Printf("Warning: failed to delete per-session network policy %s/%s: %v", namespace, npName, err)
	}
}

func allowAllEgressRules() []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{{}}
}

func denyInternetEgressRules(allowCIDRs []string) []networkingv1.NetworkPolicyEgressRule {
	return denyInternetEgressPolicy(allowCIDRs).Egress
}
