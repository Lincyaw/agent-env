package gateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	extensionsv1beta1 "sigs.k8s.io/agent-sandbox/extensions/api/v1beta1"
)

const (
	defaultGRPCAuthSecretName  = "agent-env-grpc-token"
	defaultSandboxWorkspaceDir = "/workspace"
)

func sandboxTemplateName(poolName string) string {
	return dnsLabelWithSuffix(poolName, "-template")
}

func dnsLabelWithSuffix(base, suffix string) string {
	const maxLabelBytes = 63
	base = strings.Trim(base, "-")
	if base == "" {
		base = "sandbox"
	}
	name := base + suffix
	if len(name) <= maxLabelBytes {
		return name
	}

	sum := sha256.Sum256([]byte(base))
	hash := hex.EncodeToString(sum[:])[:10]
	maxBaseBytes := maxLabelBytes - len(suffix) - len(hash) - 1
	if maxBaseBytes < 1 {
		return strings.Trim(hash+suffix, "-")
	}
	base = strings.Trim(base[:maxBaseBytes], "-")
	if base == "" {
		base = "sandbox"
	}
	return base + "-" + hash + suffix
}

func boolPtr(v bool) *bool {
	return &v
}

func int32Ptr(v int32) *int32 {
	return &v
}

func int64Ptr(v int64) *int64 {
	return &v
}

func desiredSandboxWarmPoolReplicas(pool *extensionsv1beta1.SandboxWarmPool) int32 {
	if pool.Spec.Replicas == nil {
		return 1
	}
	return *pool.Spec.Replicas
}

func (g *Gateway) sandboxNetworkPolicyManagement() extensionsv1beta1.NetworkPolicyManagement {
	switch strings.ToLower(strings.TrimSpace(g.gwConfig.SandboxNetworkPolicyManagement)) {
	case strings.ToLower(string(extensionsv1beta1.NetworkPolicyManagementManaged)):
		return extensionsv1beta1.NetworkPolicyManagementManaged
	default:
		return extensionsv1beta1.NetworkPolicyManagementUnmanaged
	}
}

func (g *Gateway) egressAllowCIDRs() []string {
	raw := strings.TrimSpace(g.gwConfig.SandboxEgressAllowCIDRs)
	if raw == "" {
		return []string{"10.0.0.0/8", "172.16.0.0/12"}
	}
	parts := strings.Split(raw, ",")
	cidrs := make([]string, 0, len(parts))
	for _, p := range parts {
		if c := strings.TrimSpace(p); c != "" {
			cidrs = append(cidrs, c)
		}
	}
	return cidrs
}

func denyInternetEgressPolicy(allowCIDRs []string) *extensionsv1beta1.NetworkPolicySpec {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	peers := make([]networkingv1.NetworkPolicyPeer, 0, len(allowCIDRs))
	for _, cidr := range allowCIDRs {
		peers = append(peers, networkingv1.NetworkPolicyPeer{
			IPBlock: &networkingv1.IPBlock{CIDR: cidr},
		})
	}
	return &extensionsv1beta1.NetworkPolicySpec{
		Egress: []networkingv1.NetworkPolicyEgressRule{
			{
				Ports: []networkingv1.NetworkPolicyPort{
					{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 53}, Protocol: &udp},
					{Port: &intstr.IntOrString{Type: intstr.Int, IntVal: 53}, Protocol: &tcp},
				},
			},
			{
				To: peers,
			},
		},
	}
}

func primarySandboxTemplateImage(template *extensionsv1beta1.SandboxTemplate) string {
	for _, container := range template.Spec.PodTemplate.Spec.Containers {
		if container.Name != "sidecar" {
			return container.Image
		}
	}
	return ""
}

func (g *Gateway) sandboxPodSpec(
	image string,
	workspaceDir string,
	resources corev1.ResourceRequirements,
	privateContainers []PrivateContainerSpec,
) corev1.PodSpec {
	if workspaceDir == "" {
		workspaceDir = defaultSandboxWorkspaceDir
	}
	sidecarHTTPPort := g.gwConfig.SidecarHTTPPort
	if sidecarHTTPPort == 0 {
		sidecarHTTPPort = 8080
	}
	sidecarGRPCPort := g.gwConfig.SidecarGRPCPort
	if sidecarGRPCPort == 0 {
		sidecarGRPCPort = 9090
	}
	sidecarImage := g.gwConfig.SidecarImage
	if sidecarImage == "" {
		sidecarImage = "arl-sidecar:latest"
	}
	executorAgentImage := g.gwConfig.ExecutorAgentImage
	if executorAgentImage == "" {
		executorAgentImage = "arl-executor-agent:latest"
	}

	automount := false
	executorCommand := "exec /arl-bin/executor-agent --socket=/var/run/arl/exec.sock --workspace=" + shellQuote(workspaceDir)
	pod := corev1.PodSpec{
		AutomountServiceAccountToken: &automount,
		InitContainers: []corev1.Container{
			{
				Name:            "copy-executor-agent",
				Image:           executorAgentImage,
				ImagePullPolicy: g.injectedPullPolicy(),
				Command:         []string{"cp", "/executor-agent", "/arl-bin/executor-agent"},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "arl-bin", MountPath: "/arl-bin"},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Name:            "executor",
				Image:           image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/bin/sh", "-c", executorCommand},
				Env:             g.executorEnv(),
				Resources:       g.ensureEphemeralStorage(resources),
				VolumeMounts: []corev1.VolumeMount{
					{Name: "arl-bin", MountPath: "/arl-bin"},
					{Name: "arl-socket", MountPath: "/var/run/arl"},
				},
			},
			{
				Name:            "sidecar",
				Image:           sidecarImage,
				ImagePullPolicy: g.injectedPullPolicy(),
				Command:         []string{"/sidecar"},
				Args: []string{
					"--workspace=" + workspaceDir,
					fmt.Sprintf("--http-port=%d", sidecarHTTPPort),
					fmt.Sprintf("--grpc-port=%d", sidecarGRPCPort),
				},
				Env: g.sidecarEnv(),
				Ports: []corev1.ContainerPort{
					{Name: "http", ContainerPort: int32(sidecarHTTPPort), Protocol: corev1.ProtocolTCP},
					{Name: "grpc", ContainerPort: int32(sidecarGRPCPort), Protocol: corev1.ProtocolTCP},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "arl-socket", MountPath: "/var/run/arl"},
				},
				StartupProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(int32(sidecarHTTPPort))},
					},
					PeriodSeconds:    2,
					FailureThreshold: 30,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromInt32(int32(sidecarHTTPPort))},
					},
					PeriodSeconds:    5,
					FailureThreshold: 3,
				},
				LivenessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt32(int32(sidecarGRPCPort))},
					},
					PeriodSeconds:    10,
					FailureThreshold: 3,
				},
			},
		},
		Volumes: []corev1.Volume{
			{Name: "arl-bin", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			{Name: "arl-socket", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
		},
	}
	if g.gwConfig.SandboxCheckpointEnabled {
		pod.Volumes = append(pod.Volumes, corev1.Volume{
			Name:         "checkpoint-scratch",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		for i := range pod.Containers {
			switch pod.Containers[i].Name {
			case "executor":
				pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, corev1.VolumeMount{
					Name:      "checkpoint-scratch",
					MountPath: "/mnt/arl-checkpoint",
				})
				pod.Containers[i].Env = append(pod.Containers[i].Env, corev1.EnvVar{
					Name:  "ARL_CHECKPOINT_ENABLED",
					Value: "1",
				})
			case "sidecar":
				pod.Containers[i].VolumeMounts = append(pod.Containers[i].VolumeMounts, corev1.VolumeMount{
					Name:      "checkpoint-scratch",
					MountPath: "/mnt/arl-checkpoint",
				})
				pod.Containers[i].Env = append(pod.Containers[i].Env, corev1.EnvVar{
					Name:  "ARL_CHECKPOINT_DIR",
					Value: "/mnt/arl-checkpoint",
				})
			}
		}
	}
	if schedulerName := strings.TrimSpace(g.gwConfig.SchedulerName); schedulerName != "" {
		pod.SchedulerName = schedulerName
	}
	for _, privateContainer := range privateContainers {
		pod.Containers = append(
			pod.Containers,
			g.sandboxPrivateContainer(privateContainer, workspaceDir),
		)
	}
	if runtimeClassName := strings.TrimSpace(g.gwConfig.SandboxRuntimeClassName); runtimeClassName != "" {
		pod.RuntimeClassName = &runtimeClassName
	}
	if seccomp := g.sandboxSeccompProfile(); seccomp != nil {
		pod.SecurityContext = &corev1.PodSecurityContext{SeccompProfile: seccomp}
	}
	g.applyContainerSecurityPolicy(&pod)
	g.injectProxyEnv(&pod)
	return pod
}

func (g *Gateway) sandboxPrivateContainer(spec PrivateContainerSpec, workspaceDir string) corev1.Container {
	container := corev1.Container{
		Name:            spec.Name,
		Image:           spec.Image,
		ImagePullPolicy: privateContainerPullPolicy(spec.ImagePullPolicy, g.injectedPullPolicy()),
		Command:         spec.Command,
		Args:            spec.Args,
		Env:             privateContainerEnv(spec.Env),
	}
	if len(container.Command) == 0 && len(container.Args) == 0 {
		container.Command = []string{"sh", "-c", "sleep infinity"}
	}
	if spec.Resources != nil {
		container.Resources = g.ensureEphemeralStorage(*spec.Resources)
	}
	if spec.MountWorkspace {
		mountPath := strings.TrimSpace(spec.WorkspaceMountPath)
		if mountPath == "" {
			mountPath = workspaceDir
		}
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name:      "workspace",
			MountPath: mountPath,
			ReadOnly:  strings.EqualFold(strings.TrimSpace(spec.WorkspaceAccess), "readonly"),
		})
	}
	return container
}

func privateContainerPullPolicy(value string, fallback corev1.PullPolicy) corev1.PullPolicy {
	switch corev1.PullPolicy(strings.TrimSpace(value)) {
	case corev1.PullAlways:
		return corev1.PullAlways
	case corev1.PullIfNotPresent:
		return corev1.PullIfNotPresent
	case corev1.PullNever:
		return corev1.PullNever
	default:
		return fallback
	}
}

func privateContainerEnv(env map[string]string) []corev1.EnvVar {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]corev1.EnvVar, 0, len(env))
	for _, key := range keys {
		out = append(out, corev1.EnvVar{Name: key, Value: env[key]})
	}
	return out
}

func (g *Gateway) sandboxSeccompProfile() *corev1.SeccompProfile {
	profileType := strings.TrimSpace(g.gwConfig.SandboxSeccompProfileType)
	if profileType == "" {
		profileType = string(corev1.SeccompProfileTypeRuntimeDefault)
	}
	switch strings.ToLower(profileType) {
	case strings.ToLower(string(corev1.SeccompProfileTypeRuntimeDefault)):
		return &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	case strings.ToLower(string(corev1.SeccompProfileTypeUnconfined)):
		return &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}
	case strings.ToLower(string(corev1.SeccompProfileTypeLocalhost)):
		localhostProfile := strings.TrimSpace(g.gwConfig.SandboxSeccompLocalhostProfile)
		return &corev1.SeccompProfile{
			Type:             corev1.SeccompProfileTypeLocalhost,
			LocalhostProfile: &localhostProfile,
		}
	default:
		return &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	}
}

func (g *Gateway) applyContainerSecurityPolicy(pod *corev1.PodSpec) {
	allowPrivilegeEscalation := g.gwConfig.SandboxAllowPrivilegeEscalation
	apply := func(container *corev1.Container) {
		if container.SecurityContext == nil {
			container.SecurityContext = &corev1.SecurityContext{}
		}
		container.SecurityContext.AllowPrivilegeEscalation = boolPtr(allowPrivilegeEscalation)
	}
	for i := range pod.InitContainers {
		apply(&pod.InitContainers[i])
	}
	for i := range pod.Containers {
		apply(&pod.Containers[i])
		if g.gwConfig.SandboxCheckpointEnabled && pod.Containers[i].Name == "executor" {
			sc := pod.Containers[i].SecurityContext
			if sc.Capabilities == nil {
				sc.Capabilities = &corev1.Capabilities{}
			}
			sc.Capabilities.Add = append(sc.Capabilities.Add, "SYS_ADMIN")
			unconfined := corev1.AppArmorProfileTypeUnconfined
			sc.AppArmorProfile = &corev1.AppArmorProfile{Type: unconfined}
		}
	}
}

func (g *Gateway) injectedPullPolicy() corev1.PullPolicy {
	switch corev1.PullPolicy(g.gwConfig.ImagePullPolicy) {
	case corev1.PullIfNotPresent:
		return corev1.PullIfNotPresent
	case corev1.PullNever:
		return corev1.PullNever
	default:
		return corev1.PullAlways
	}
}

func (g *Gateway) grpcAuthSecretName() string {
	if name := strings.TrimSpace(g.gwConfig.GRPCAuthSecretName); name != "" {
		return name
	}
	return defaultGRPCAuthSecretName
}

func (g *Gateway) sidecarEnv() []corev1.EnvVar {
	protocol := g.gwConfig.ExecutorProtocol
	if protocol == "" {
		protocol = "v1"
	}
	return []corev1.EnvVar{
		{
			Name: "GRPC_AUTH_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: g.grpcAuthSecretName()},
					Key:                  "token",
					Optional:             boolPtr(false),
				},
			},
		},
		{Name: "EXECUTOR_PROTOCOL", Value: protocol},
	}
}

func (g *Gateway) executorEnv() []corev1.EnvVar {
	protocol := g.gwConfig.ExecutorProtocol
	if protocol == "" {
		protocol = "v1"
	}
	envs := []corev1.EnvVar{
		{Name: "EXECUTOR_PROTOCOL", Value: protocol},
	}
	if g.gwConfig.IrohRelayURL != "" {
		envs = append(envs, corev1.EnvVar{Name: "IROH_RELAY_URL", Value: g.gwConfig.IrohRelayURL})
	}
	return envs
}

func (g *Gateway) injectProxyEnv(pod *corev1.PodSpec) {
	if g.gwConfig.PodHTTPProxy == "" {
		return
	}
	noProxy := g.gwConfig.PodNoProxy
	if noProxy == "" {
		noProxy = "localhost,127.0.0.1,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,.svc,.svc.cluster.local"
	}
	envVars := []corev1.EnvVar{
		{Name: "HTTP_PROXY", Value: g.gwConfig.PodHTTPProxy},
		{Name: "HTTPS_PROXY", Value: g.gwConfig.PodHTTPProxy},
		{Name: "http_proxy", Value: g.gwConfig.PodHTTPProxy},
		{Name: "https_proxy", Value: g.gwConfig.PodHTTPProxy},
		{Name: "NO_PROXY", Value: noProxy},
		{Name: "no_proxy", Value: noProxy},
	}
	for i := range pod.InitContainers {
		for _, ev := range envVars {
			upsertEnv(&pod.InitContainers[i].Env, ev)
		}
	}
	for i := range pod.Containers {
		if pod.Containers[i].Name == "sidecar" {
			continue
		}
		for _, ev := range envVars {
			upsertEnv(&pod.Containers[i].Env, ev)
		}
	}
}

func (g *Gateway) ensureEphemeralStorage(resources corev1.ResourceRequirements) corev1.ResourceRequirements {
	limitStr := g.gwConfig.DefaultEphemeralStorageLimit
	if limitStr == "" {
		limitStr = "10Gi"
	}
	requestStr := g.gwConfig.DefaultEphemeralStorageRequest
	if requestStr == "" {
		requestStr = "100Mi"
	}
	if resources.Limits == nil {
		resources.Limits = corev1.ResourceList{}
	}
	if _, ok := resources.Limits[corev1.ResourceEphemeralStorage]; !ok {
		resources.Limits[corev1.ResourceEphemeralStorage] = resource.MustParse(limitStr)
	}
	if resources.Requests == nil {
		resources.Requests = corev1.ResourceList{}
	}
	if _, ok := resources.Requests[corev1.ResourceEphemeralStorage]; !ok {
		resources.Requests[corev1.ResourceEphemeralStorage] = resource.MustParse(requestStr)
	}
	return resources
}

func upsertEnv(envs *[]corev1.EnvVar, ev corev1.EnvVar) {
	for i := range *envs {
		if (*envs)[i].Name == ev.Name {
			(*envs)[i] = ev
			return
		}
	}
	*envs = append(*envs, ev)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func (g *Gateway) ensureSandboxRuntimeSecret(ctx context.Context, namespace string) error {
	if g.gwConfig.GRPCAuthToken == "" {
		return fmt.Errorf("GRPCAuthToken is required for sandbox-backed pools")
	}
	secret := &corev1.Secret{}
	secretName := g.grpcAuthSecretName()
	key := types.NamespacedName{Name: secretName, Namespace: namespace}
	if err := g.k8sClient.Get(ctx, key, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("get gRPC auth secret: %w", err)
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
			Type:       corev1.SecretTypeOpaque,
			Data:       map[string][]byte{"token": []byte(g.gwConfig.GRPCAuthToken)},
		}
		return g.k8sClient.Create(ctx, secret)
	}
	if string(secret.Data["token"]) == g.gwConfig.GRPCAuthToken {
		return nil
	}
	patch := client.MergeFrom(secret.DeepCopy())
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data["token"] = []byte(g.gwConfig.GRPCAuthToken)
	return g.k8sClient.Patch(ctx, secret, patch)
}
