package gateway

import (
	"context"
	"fmt"
	"log"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// sessionCiliumNetworkPolicyName returns the CiliumNetworkPolicy name for a
// per-session FQDN-based egress policy.
func sessionCiliumNetworkPolicyName(sessionID string) string {
	return dnsLabelWithSuffix(sessionID, "-cnp")
}

var ciliumNetworkPolicyGVK = schema.GroupVersionKind{
	Group:   "cilium.io",
	Version: "v2",
	Kind:    "CiliumNetworkPolicy",
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
		g.deleteCiliumNetworkPolicy(ctx, sessionID, ns)
		return g.applySessionNetworkPolicy(ctx, ns, npName, claimUID, sessionID, allowAllEgressRules())
	}

	cidrs := req.EgressCIDRs
	if len(cidrs) == 0 {
		cidrs = g.egressAllowCIDRs()
	}

	if len(req.EgressFQDNs) > 0 {
		if err := g.applyCiliumNetworkPolicy(ctx, ns, claimUID, sessionID, req.EgressFQDNs, cidrs); err != nil {
			return fmt.Errorf("apply cilium network policy for session %s: %w", sessionID, err)
		}
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
	g.deleteCiliumNetworkPolicy(ctx, sessionID, namespace)
}

func allowAllEgressRules() []networkingv1.NetworkPolicyEgressRule {
	return []networkingv1.NetworkPolicyEgressRule{{}}
}

func denyInternetEgressRules(allowCIDRs []string) []networkingv1.NetworkPolicyEgressRule {
	return denyInternetEgressPolicy(allowCIDRs).Egress
}

// applyCiliumNetworkPolicy creates or updates a CiliumNetworkPolicy that allows
// egress to the given FQDNs and CIDRs. This uses unstructured objects to avoid
// a Go dependency on the Cilium types.
func (g *Gateway) applyCiliumNetworkPolicy(
	ctx context.Context,
	namespace, claimUID, sessionID string,
	fqdns, cidrs []string,
) error {
	cnpName := sessionCiliumNetworkPolicyName(sessionID)

	// Build toFQDNs entries: matchPattern for wildcards, matchName for exact.
	fqdnEntries := make([]interface{}, 0, len(fqdns))
	for _, fqdn := range fqdns {
		fqdn = strings.TrimSpace(fqdn)
		if fqdn == "" {
			continue
		}
		if strings.HasPrefix(fqdn, "*") {
			fqdnEntries = append(fqdnEntries, map[string]interface{}{
				"matchPattern": fqdn,
			})
		} else {
			fqdnEntries = append(fqdnEntries, map[string]interface{}{
				"matchName": fqdn,
			})
		}
	}

	egress := []interface{}{
		map[string]interface{}{
			"toEndpoints": []interface{}{
				map[string]interface{}{
					"matchLabels": map[string]interface{}{
						"k8s:io.kubernetes.pod.namespace": "kube-system",
						"k8s-app":                         "kube-dns",
					},
				},
			},
			"toPorts": []interface{}{
				map[string]interface{}{
					"ports": []interface{}{
						map[string]interface{}{"port": "53", "protocol": "ANY"},
					},
					"rules": map[string]interface{}{
						"dns": []interface{}{
							map[string]interface{}{"matchPattern": "*"},
						},
					},
				},
			},
		},
	}

	if len(fqdnEntries) > 0 {
		egress = append(egress, map[string]interface{}{
			"toFQDNs": fqdnEntries,
		})
	}

	if len(cidrs) > 0 {
		cidrList := make([]interface{}, len(cidrs))
		for i, c := range cidrs {
			cidrList[i] = c
		}
		egress = append(egress, map[string]interface{}{
			"toCIDR": cidrList,
		})
	}

	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "cilium.io/v2",
			"kind":       "CiliumNetworkPolicy",
			"metadata": map[string]interface{}{
				"name":      cnpName,
				"namespace": namespace,
				"labels": map[string]interface{}{
					labels.SessionAnnotation: sessionID,
				},
			},
			"spec": map[string]interface{}{
				"endpointSelector": map[string]interface{}{
					"matchLabels": map[string]interface{}{
						extensionsv1beta1.SandboxIDLabel: claimUID,
					},
				},
				"egress": egress,
			},
		},
	}
	obj.SetGroupVersionKind(ciliumNetworkPolicyGVK)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	err := g.k8sClient.Get(ctx, types.NamespacedName{Name: cnpName, Namespace: namespace}, existing)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get cilium network policy %s/%s: %w", namespace, cnpName, err)
		}
		if err := g.k8sClient.Create(ctx, obj); err != nil {
			return fmt.Errorf("create cilium network policy %s/%s: %w", namespace, cnpName, err)
		}
		log.Printf("Created CiliumNetworkPolicy %s/%s for session %s", namespace, cnpName, sessionID)
		return nil
	}

	patch := client.MergeFrom(existing.DeepCopy())
	existingMeta, _ := existing.Object["metadata"].(map[string]interface{})
	if existingMeta != nil {
		existingMeta["labels"] = map[string]interface{}{
			labels.SessionAnnotation: sessionID,
		}
	}
	existing.Object["spec"] = obj.Object["spec"]
	if err := g.k8sClient.Patch(ctx, existing, patch); err != nil {
		return fmt.Errorf("update cilium network policy %s/%s: %w", namespace, cnpName, err)
	}
	log.Printf("Updated CiliumNetworkPolicy %s/%s for session %s", namespace, cnpName, sessionID)
	return nil
}

// deleteCiliumNetworkPolicy removes the per-session CiliumNetworkPolicy.
// It is safe to call when no policy exists.
func (g *Gateway) deleteCiliumNetworkPolicy(ctx context.Context, sessionID, namespace string) {
	cnpName := sessionCiliumNetworkPolicyName(sessionID)
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(ciliumNetworkPolicyGVK)
	obj.SetName(cnpName)
	obj.SetNamespace(namespace)
	if err := g.k8sClient.Delete(ctx, obj); err != nil && !apierrors.IsNotFound(err) {
		log.Printf("Warning: failed to delete CiliumNetworkPolicy %s/%s: %v", namespace, cnpName, err)
	}
}
