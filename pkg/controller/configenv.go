package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	configenvutil "github.com/Lincyaw/agent-env/pkg/configenv"
)

const (
	configEnvHashAnnotation  = configenvutil.HashAnnotation
	configEnvManagedLabel    = "arl.infra.io/config-env-managed"
	configEnvResourceLabel   = "arl.infra.io/config-env-resource"
	configEnvResourceKindTag = "arl.infra.io/config-env-kind"
)

type renderedConfigEnv struct {
	spec        *arlv1alpha1.ConfigEnvSpec
	hash        string
	configMaps  []configResourceRef
	secrets     []configResourceRef
	resourceSet map[string]struct{}
}

type configResourceRef struct {
	name string
	kind string
}

func (r *WarmPoolReconciler) resolveConfigEnvSpec(pool *arlv1alpha1.WarmPool) (*arlv1alpha1.ConfigEnvSpec, error) {
	return configenvutil.ResolveSpec(pool)
}

func (r *WarmPoolReconciler) reconcileConfigEnv(ctx context.Context, pool *arlv1alpha1.WarmPool, cfg *arlv1alpha1.ConfigEnvSpec) (*renderedConfigEnv, error) {
	if cfg == nil {
		if err := r.deleteStaleConfigEnvResources(ctx, pool, nil); err != nil {
			return nil, err
		}
		pool.Status.ConfigEnv = nil
		setCondition(&pool.Status.Conditions, configenvutil.ReadyConditionType, metav1.ConditionTrue, "ConfigEnvDisabled", "ConfigEnv not configured")
		return nil, nil
	}

	rendered, err := configenvutil.RenderSpec(cfg)
	if err != nil {
		return nil, err
	}
	if err := validateRenderedMountConflicts(pool, rendered); err != nil {
		return nil, err
	}

	result := &renderedConfigEnv{
		spec:        rendered,
		hash:        configenvutil.HashSpec(rendered),
		resourceSet: make(map[string]struct{}),
	}

	for i := range rendered.ConfigMaps {
		name := managedConfigEnvName(pool.Name, rendered.ConfigMaps[i].Name)
		if err := r.applyManagedConfigMap(ctx, pool, &rendered.ConfigMaps[i], name); err != nil {
			return nil, err
		}
		ref := configResourceRef{name: name, kind: "ConfigMap"}
		result.configMaps = append(result.configMaps, ref)
		result.resourceSet[ref.kind+"/"+ref.name] = struct{}{}
	}

	for i := range rendered.Secrets {
		name := managedConfigEnvName(pool.Name, rendered.Secrets[i].Name)
		if err := r.applyManagedSecret(ctx, pool, &rendered.Secrets[i], name); err != nil {
			return nil, err
		}
		ref := configResourceRef{name: name, kind: "Secret"}
		result.secrets = append(result.secrets, ref)
		result.resourceSet[ref.kind+"/"+ref.name] = struct{}{}
	}

	if err := r.deleteStaleConfigEnvResources(ctx, pool, result.resourceSet); err != nil {
		return nil, err
	}

	status := &arlv1alpha1.ConfigEnvStatus{
		Phase: arlv1alpha1.ConfigEnvPhaseReady,
	}
	for _, ref := range result.configMaps {
		status.ConfigMapRefs = append(status.ConfigMapRefs, arlv1alpha1.ConfigEnvResourceRef{
			Name:      ref.name,
			Namespace: pool.Namespace,
			Kind:      ref.kind,
		})
	}
	for _, ref := range result.secrets {
		status.SecretRefs = append(status.SecretRefs, arlv1alpha1.ConfigEnvResourceRef{
			Name:      ref.name,
			Namespace: pool.Namespace,
			Kind:      ref.kind,
		})
	}
	setCondition(&status.Conditions, "Ready", metav1.ConditionTrue, "ConfigEnvReady", "ConfigEnv resources rendered and applied")
	pool.Status.ConfigEnv = status
	setCondition(&pool.Status.Conditions, configenvutil.ReadyConditionType, metav1.ConditionTrue, "ConfigEnvReady", "ConfigEnv resources rendered and applied")

	return result, nil
}

func (r *WarmPoolReconciler) setConfigEnvFailureStatus(pool *arlv1alpha1.WarmPool, message string) {
	status := &arlv1alpha1.ConfigEnvStatus{
		Phase: arlv1alpha1.ConfigEnvPhaseFailed,
	}
	if pool.Status.ConfigEnv != nil {
		status.ConfigMapRefs = pool.Status.ConfigEnv.ConfigMapRefs
		status.SecretRefs = pool.Status.ConfigEnv.SecretRefs
		status.Conditions = pool.Status.ConfigEnv.Conditions
	}
	setCondition(&status.Conditions, "Ready", metav1.ConditionFalse, "ConfigEnvFailed", message)
	pool.Status.ConfigEnv = status
	setCondition(&pool.Status.Conditions, configenvutil.ReadyConditionType, metav1.ConditionFalse, "ConfigEnvFailed", message)
	setCondition(&pool.Status.Conditions, "Ready", metav1.ConditionFalse, "ConfigEnvFailed", message)
}

func managedConfigEnvName(poolName, base string) string {
	name := fmt.Sprintf("%s-%s", poolName, base)
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:4])
	prefix := name[:63-len(suffix)-1]
	return prefix + "-" + suffix
}

func managedVolumeName(prefix, resourceName string) string {
	name := fmt.Sprintf("%s-%s", prefix, resourceName)
	if len(name) <= 63 {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := hex.EncodeToString(sum[:4])
	prefixPart := name[:63-len(suffix)-1]
	return prefixPart + "-" + suffix
}

func (r *WarmPoolReconciler) deleteStaleConfigPod(ctx context.Context, pod *corev1.Pod, desiredHash string) error {
	current := &corev1.Pod{}
	if err := r.Get(ctx, client.ObjectKeyFromObject(pod), current); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if current.DeletionTimestamp != nil {
		return nil
	}
	if current.Annotations[configEnvHashAnnotation] == desiredHash {
		return nil
	}
	if current.Labels[StatusLabelKey] == StatusAllocated {
		return nil
	}
	if current.Labels[StatusLabelKey] == StatusIdle {
		okToDelete, err := r.markPodRecycling(ctx, current)
		if err != nil {
			return err
		}
		if !okToDelete {
			return nil
		}
		if current.Labels == nil {
			current.Labels = map[string]string{}
		}
		current.Labels[StatusLabelKey] = StatusRecycling
	}
	if err := r.Delete(ctx, current); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *WarmPoolReconciler) markPodRecycling(ctx context.Context, pod *corev1.Pod) (bool, error) {
	patch := []byte(fmt.Sprintf(
		`[{"op":"test","path":"/metadata/labels/%s","value":"%s"},{"op":"replace","path":"/metadata/labels/%s","value":"%s"}]`,
		jsonPatchEscape(StatusLabelKey),
		StatusIdle,
		jsonPatchEscape(StatusLabelKey),
		StatusRecycling,
	))
	if err := r.Patch(ctx, pod, client.RawPatch(types.JSONPatchType, patch)); err != nil {
		if errors.IsConflict(err) || errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func jsonPatchEscape(path string) string {
	replacer := strings.NewReplacer("~", "~0", "/", "~1")
	return replacer.Replace(path)
}

func (r *WarmPoolReconciler) applyManagedConfigMap(ctx context.Context, pool *arlv1alpha1.WarmPool, tmpl *arlv1alpha1.ConfigMapTemplate, name string) error {
	obj := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pool.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Data = mapsCopyString(tmpl.Data)
		obj.BinaryData = mapsCopyBinaryData(tmpl.BinaryData)
		obj.Immutable = tmpl.Immutable
		obj.Labels = mergeStringMaps(obj.Labels, map[string]string{
			PoolLabelKey:             pool.Name,
			configEnvManagedLabel:    "true",
			configEnvResourceKindTag: "ConfigMap",
			configEnvResourceLabel:   tmpl.Name,
		})
		if err := controllerutil.SetControllerReference(pool, obj, r.Scheme); err != nil {
			return err
		}
		return nil
	})
	return err
}

func (r *WarmPoolReconciler) applyManagedSecret(ctx context.Context, pool *arlv1alpha1.WarmPool, tmpl *arlv1alpha1.SecretTemplate, name string) error {
	obj := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: pool.Namespace}}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, obj, func() error {
		obj.Type = tmpl.Type
		obj.Data = mapsCopyBinaryData(tmpl.Data)
		if obj.Data == nil {
			obj.Data = map[string][]byte{}
		}
		for key, value := range tmpl.StringData {
			obj.Data[key] = []byte(value)
		}
		obj.Immutable = tmpl.Immutable
		obj.Labels = mergeStringMaps(obj.Labels, map[string]string{
			PoolLabelKey:             pool.Name,
			configEnvManagedLabel:    "true",
			configEnvResourceKindTag: "Secret",
			configEnvResourceLabel:   tmpl.Name,
		})
		if err := controllerutil.SetControllerReference(pool, obj, r.Scheme); err != nil {
			return err
		}
		return nil
	})
	return err
}

func (r *WarmPoolReconciler) deleteStaleConfigEnvResources(ctx context.Context, pool *arlv1alpha1.WarmPool, keep map[string]struct{}) error {
	var cms corev1.ConfigMapList
	if err := r.List(ctx, &cms, client.InNamespace(pool.Namespace), client.MatchingLabels{
		PoolLabelKey:             pool.Name,
		configEnvManagedLabel:    "true",
		configEnvResourceKindTag: "ConfigMap",
	}); err != nil {
		return err
	}
	for i := range cms.Items {
		key := "ConfigMap/" + cms.Items[i].Name
		if keep != nil {
			if _, ok := keep[key]; ok {
				continue
			}
		}
		if err := r.Delete(ctx, &cms.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	var secrets corev1.SecretList
	if err := r.List(ctx, &secrets, client.InNamespace(pool.Namespace), client.MatchingLabels{
		PoolLabelKey:             pool.Name,
		configEnvManagedLabel:    "true",
		configEnvResourceKindTag: "Secret",
	}); err != nil {
		return err
	}
	for i := range secrets.Items {
		key := "Secret/" + secrets.Items[i].Name
		if keep != nil {
			if _, ok := keep[key]; ok {
				continue
			}
		}
		if err := r.Delete(ctx, &secrets.Items[i]); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}
	return nil
}

func (r *WarmPoolReconciler) injectConfigEnv(pod *corev1.Pod, rendered *renderedConfigEnv) {
	if rendered == nil || rendered.spec == nil {
		delete(pod.Annotations, configEnvHashAnnotation)
		return
	}

	if pod.Annotations == nil {
		pod.Annotations = map[string]string{}
	}
	pod.Annotations[configEnvHashAnnotation] = rendered.hash

	if len(rendered.spec.EnvVars) > 0 {
		target := executorContainerName(pod)
		if target != "" {
			for i := range pod.Spec.Containers {
				if pod.Spec.Containers[i].Name == target {
					pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, rendered.spec.EnvVars...)
					break
				}
			}
		}
	}

	for _, tmpl := range rendered.spec.ConfigMaps {
		if tmpl.Inject == nil {
			continue
		}
		resourceName := managedConfigEnvName(pod.Labels[PoolLabelKey], tmpl.Name)
		volumeName := managedVolumeName("cm", resourceName)
		readOnly := true
		if tmpl.Inject.ReadOnly != nil {
			readOnly = *tmpl.Inject.ReadOnly
		}
		upsertVolume(&pod.Spec.Volumes, corev1.Volume{
			Name: volumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: resourceName},
				},
			},
		})
		if target := desiredContainerName(pod, tmpl.Inject.Container); target != "" {
			appendVolumeMount(pod, target, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: tmpl.Inject.MountPath,
				ReadOnly:  readOnly,
				SubPath:   tmpl.Inject.SubPath,
			})
		}
	}

	for _, tmpl := range rendered.spec.Secrets {
		resourceName := managedConfigEnvName(pod.Labels[PoolLabelKey], tmpl.Name)
		if tmpl.Inject != nil && tmpl.Inject.Volume != nil {
			injection := tmpl.Inject.Volume
			volumeName := managedVolumeName("secret", resourceName)
			readOnly := true
			if injection.ReadOnly != nil {
				readOnly = *injection.ReadOnly
			}
			upsertVolume(&pod.Spec.Volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: resourceName,
					},
				},
			})
			if target := desiredContainerName(pod, injection.Container); target != "" {
				appendVolumeMount(pod, target, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: injection.MountPath,
					ReadOnly:  readOnly,
					SubPath:   injection.SubPath,
				})
			}
		}
		if tmpl.Inject != nil && len(tmpl.Inject.AsEnv) > 0 {
			target := executorContainerName(pod)
			for _, env := range tmpl.Inject.AsEnv {
				appendEnvVar(pod, target, corev1.EnvVar{
					Name: env.Name,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: resourceName},
							Key:                  env.Key,
						},
					},
				})
			}
		}
	}
}

func executorContainerName(pod *corev1.Pod) string {
	for _, c := range pod.Spec.Containers {
		if c.Name == "executor" {
			return c.Name
		}
	}
	for _, c := range pod.Spec.Containers {
		if c.Name != "sidecar" {
			return c.Name
		}
	}
	return ""
}

func validateRenderedMountConflicts(pool *arlv1alpha1.WarmPool, cfg *arlv1alpha1.ConfigEnvSpec) error {
	mountsByContainer := map[string]map[string]string{}
	record := func(containerName, mountPath, source string) error {
		if containerName == "" || mountPath == "" {
			return nil
		}
		if _, ok := mountsByContainer[containerName]; !ok {
			mountsByContainer[containerName] = map[string]string{}
		}
		if existing, ok := mountsByContainer[containerName][mountPath]; ok {
			return fmt.Errorf("mountPath conflict for container %q at %q between %s and %s", containerName, mountPath, existing, source)
		}
		mountsByContainer[containerName][mountPath] = source
		return nil
	}

	for _, c := range pool.Spec.Template.Spec.Containers {
		for _, mount := range c.VolumeMounts {
			if err := record(c.Name, mount.MountPath, fmt.Sprintf("template container %q", c.Name)); err != nil {
				return err
			}
		}
	}
	for _, c := range pool.Spec.Template.Spec.InitContainers {
		for _, mount := range c.VolumeMounts {
			if err := record(c.Name, mount.MountPath, fmt.Sprintf("template initContainer %q", c.Name)); err != nil {
				return err
			}
		}
	}
	defaultTarget := defaultConfigEnvTargetContainer(pool.Spec.Template.Spec.Containers)
	for i, cm := range cfg.ConfigMaps {
		if cm.Inject == nil {
			continue
		}
		containerName := cm.Inject.Container
		if containerName == "" {
			containerName = defaultTarget
		}
		if err := record(containerName, cm.Inject.MountPath, fmt.Sprintf("configMap %q", cm.Name)); err != nil {
			return fmt.Errorf("configEnv.configMaps[%d]: %w", i, err)
		}
	}
	for i, secret := range cfg.Secrets {
		if secret.Inject == nil || secret.Inject.Volume == nil {
			continue
		}
		containerName := secret.Inject.Volume.Container
		if containerName == "" {
			containerName = defaultTarget
		}
		if err := record(containerName, secret.Inject.Volume.MountPath, fmt.Sprintf("secret %q", secret.Name)); err != nil {
			return fmt.Errorf("configEnv.secrets[%d]: %w", i, err)
		}
	}
	return nil
}

func defaultConfigEnvTargetContainer(containers []corev1.Container) string {
	for _, c := range containers {
		if c.Name == "executor" {
			return c.Name
		}
	}
	for _, c := range containers {
		if c.Name != "sidecar" {
			return c.Name
		}
	}
	return ""
}

func desiredContainerName(pod *corev1.Pod, explicit string) string {
	if explicit != "" {
		return explicit
	}
	return executorContainerName(pod)
}

func upsertVolume(volumes *[]corev1.Volume, vol corev1.Volume) {
	for i := range *volumes {
		if (*volumes)[i].Name == vol.Name {
			(*volumes)[i] = vol
			return
		}
	}
	*volumes = append(*volumes, vol)
}

func appendVolumeMount(pod *corev1.Pod, containerName string, mount corev1.VolumeMount) {
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != containerName {
			continue
		}
		for j := range pod.Spec.Containers[i].VolumeMounts {
			if pod.Spec.Containers[i].VolumeMounts[j].Name == mount.Name &&
				pod.Spec.Containers[i].VolumeMounts[j].MountPath == mount.MountPath {
				pod.Spec.Containers[i].VolumeMounts[j] = mount
				return
			}
		}
		pod.Spec.Containers[i].VolumeMounts = append(pod.Spec.Containers[i].VolumeMounts, mount)
		return
	}
	for i := range pod.Spec.InitContainers {
		if pod.Spec.InitContainers[i].Name != containerName {
			continue
		}
		for j := range pod.Spec.InitContainers[i].VolumeMounts {
			if pod.Spec.InitContainers[i].VolumeMounts[j].Name == mount.Name &&
				pod.Spec.InitContainers[i].VolumeMounts[j].MountPath == mount.MountPath {
				pod.Spec.InitContainers[i].VolumeMounts[j] = mount
				return
			}
		}
		pod.Spec.InitContainers[i].VolumeMounts = append(pod.Spec.InitContainers[i].VolumeMounts, mount)
		return
	}
}

func appendEnvVar(pod *corev1.Pod, containerName string, env corev1.EnvVar) {
	if containerName == "" {
		return
	}
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != containerName {
			continue
		}
		for j := range pod.Spec.Containers[i].Env {
			if pod.Spec.Containers[i].Env[j].Name == env.Name {
				pod.Spec.Containers[i].Env[j] = env
				return
			}
		}
		pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, env)
		return
	}
}

func mapsCopyString(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out[k] = in[k]
	}
	return out
}

func mapsCopyBinaryData(in map[string][]byte) map[string][]byte {
	if in == nil {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		buf := make([]byte, len(v))
		copy(buf, v)
		out[k] = buf
	}
	return out
}

func mergeStringMaps(base, extra map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}
