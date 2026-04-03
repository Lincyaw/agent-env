package webhook

import (
	"context"
	"fmt"
	"path/filepath"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
	configenvutil "github.com/Lincyaw/agent-env/pkg/configenv"
	"github.com/Lincyaw/agent-env/pkg/interfaces"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation"
)

// WarmPoolValidator validates WarmPool resources
type WarmPoolValidator struct {
	// Add dependencies here as needed
}

// NewWarmPoolValidator creates a new WarmPool validator
func NewWarmPoolValidator() interfaces.Validator {
	return &WarmPoolValidator{}
}

// ValidateCreate validates WarmPool creation
func (v *WarmPoolValidator) ValidateCreate(ctx context.Context, obj interface{}) error {
	pool, ok := obj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object, got %T", obj)
	}

	// TODO: Implement validation logic
	// Example validations:
	// - Ensure Replicas > 0
	// - Validate PodTemplate

	if pool.Spec.Replicas <= 0 {
		return fmt.Errorf("replicas must be greater than 0")
	}

	return validateConfigEnv(pool)
}

// ValidateUpdate validates WarmPool updates
func (v *WarmPoolValidator) ValidateUpdate(ctx context.Context, oldObj, newObj interface{}) error {
	_, ok := oldObj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object for oldObj, got %T", oldObj)
	}

	newPool, ok := newObj.(*arlv1alpha1.WarmPool)
	if !ok {
		return fmt.Errorf("expected WarmPool object for newObj, got %T", newObj)
	}

	// TODO: Implement update validation

	if err := v.ValidateCreate(ctx, newPool); err != nil {
		return err
	}

	return nil
}

// ValidateDelete validates WarmPool deletion
func (v *WarmPoolValidator) ValidateDelete(ctx context.Context, obj interface{}) error {
	// TODO: Implement deletion validation if needed
	// Example: prevent deletion if pool has allocated sandboxes
	return nil
}

func validateConfigEnv(pool *arlv1alpha1.WarmPool) error {
	cfg := pool.Spec.ConfigEnv
	if cfg == nil {
		return nil
	}

	containerNames := make(map[string]struct{}, len(pool.Spec.Template.Spec.Containers)+len(pool.Spec.Template.Spec.InitContainers))
	for _, c := range pool.Spec.Template.Spec.Containers {
		containerNames[c.Name] = struct{}{}
	}
	for _, c := range pool.Spec.Template.Spec.InitContainers {
		containerNames[c.Name] = struct{}{}
	}

	if err := validateConfigEnvVars(cfg.Vars); err != nil {
		return err
	}
	rendered, err := renderConfigEnvForValidation(cfg)
	if err != nil {
		return err
	}
	if err := validateAdditionalEnvVars(rendered.EnvVars); err != nil {
		return err
	}
	if err := validateConfigMaps(pool, rendered.ConfigMaps, containerNames); err != nil {
		return err
	}
	if err := validateSecrets(pool, rendered.Secrets, containerNames); err != nil {
		return err
	}
	if err := validateMountPathConflicts(pool, rendered); err != nil {
		return err
	}

	return nil
}

func validateConfigEnvVars(vars map[string]string) error {
	for k := range vars {
		if k == "" {
			return fmt.Errorf("configEnv.vars contains an empty key")
		}
	}
	return nil
}

func validateAdditionalEnvVars(envVars []corev1.EnvVar) error {
	for i, envVar := range envVars {
		if errs := validation.IsEnvVarName(envVar.Name); len(errs) > 0 {
			return fmt.Errorf("configEnv.envVars[%d].name: %s", i, errs[0])
		}
	}
	return nil
}

func validateConfigMaps(pool *arlv1alpha1.WarmPool, configMaps []arlv1alpha1.ConfigMapTemplate, containerNames map[string]struct{}) error {
	seen := make(map[string]struct{}, len(configMaps))
	for i, cm := range configMaps {
		name := cm.Name
		if name == "" {
			return fmt.Errorf("configEnv.configMaps[%d].metadata.name is required", i)
		}
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("configEnv.configMaps[%d].metadata.name: %s", i, errs[0])
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("configEnv.configMaps[%d].metadata.name duplicates another ConfigMap name %q", i, name)
		}
		seen[name] = struct{}{}

		if cm.Namespace != "" && cm.Namespace != pool.Namespace {
			return fmt.Errorf("configEnv.configMaps[%d].metadata.namespace must be empty or %q", i, pool.Namespace)
		}
		if len(cm.Data) == 0 && len(cm.BinaryData) == 0 {
			return fmt.Errorf("configEnv.configMaps[%d] must define data or binaryData", i)
		}
		for key := range cm.Data {
			if errs := validation.IsConfigMapKey(key); len(errs) > 0 {
				return fmt.Errorf("configEnv.configMaps[%d].data[%q]: %s", i, key, errs[0])
			}
		}
		for key := range cm.BinaryData {
			if errs := validation.IsConfigMapKey(key); len(errs) > 0 {
				return fmt.Errorf("configEnv.configMaps[%d].binaryData[%q]: %s", i, key, errs[0])
			}
		}

		if cm.Inject == nil {
			return fmt.Errorf("configEnv.configMaps[%d].inject is required", i)
		}
		if err := validateVolumeInjection(fmt.Sprintf("configEnv.configMaps[%d].inject", i), *cm.Inject, containerNames); err != nil {
			return err
		}
	}
	return nil
}

func validateSecrets(pool *arlv1alpha1.WarmPool, secrets []arlv1alpha1.SecretTemplate, containerNames map[string]struct{}) error {
	seen := make(map[string]struct{}, len(secrets))
	for i, secret := range secrets {
		name := secret.Name
		if name == "" {
			return fmt.Errorf("configEnv.secrets[%d].metadata.name is required", i)
		}
		if errs := validation.IsDNS1123Subdomain(name); len(errs) > 0 {
			return fmt.Errorf("configEnv.secrets[%d].metadata.name: %s", i, errs[0])
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("configEnv.secrets[%d].metadata.name duplicates another Secret name %q", i, name)
		}
		seen[name] = struct{}{}

		if secret.Namespace != "" && secret.Namespace != pool.Namespace {
			return fmt.Errorf("configEnv.secrets[%d].metadata.namespace must be empty or %q", i, pool.Namespace)
		}
		if len(secret.Data) == 0 && len(secret.StringData) == 0 {
			return fmt.Errorf("configEnv.secrets[%d] must define data or stringData", i)
		}
		for key := range secret.Data {
			if errs := validation.IsConfigMapKey(key); len(errs) > 0 {
				return fmt.Errorf("configEnv.secrets[%d].data[%q]: %s", i, key, errs[0])
			}
		}
		for key := range secret.StringData {
			if errs := validation.IsConfigMapKey(key); len(errs) > 0 {
				return fmt.Errorf("configEnv.secrets[%d].stringData[%q]: %s", i, key, errs[0])
			}
		}

		if secret.Inject == nil {
			return fmt.Errorf("configEnv.secrets[%d].inject is required", i)
		}
		if secret.Inject.Volume == nil && len(secret.Inject.AsEnv) == 0 {
			return fmt.Errorf("configEnv.secrets[%d].inject must define volume or asEnv", i)
		}
		if secret.Inject.Volume != nil {
			if err := validateVolumeInjection(fmt.Sprintf("configEnv.secrets[%d].inject.volume", i), *secret.Inject.Volume, containerNames); err != nil {
				return err
			}
		}
		if err := validateSecretEnvVars(i, secret.Inject.AsEnv); err != nil {
			return err
		}
	}
	return nil
}

func validateVolumeInjection(prefix string, injection arlv1alpha1.VolumeInjection, containerNames map[string]struct{}) error {
	if injection.Container != "" {
		if _, ok := containerNames[injection.Container]; !ok {
			return fmt.Errorf("%s.container %q is not defined in the pod template", prefix, injection.Container)
		}
	}
	if injection.MountPath == "" {
		return fmt.Errorf("%s.mountPath is required", prefix)
	}
	if !filepath.IsAbs(injection.MountPath) {
		return fmt.Errorf("%s.mountPath must be an absolute path", prefix)
	}
	if len(injection.SubPath) > 0 && filepath.IsAbs(injection.SubPath) {
		return fmt.Errorf("%s.subPath must be relative", prefix)
	}
	return nil
}

func validateMountPathConflicts(pool *arlv1alpha1.WarmPool, cfg *arlv1alpha1.ConfigEnvSpec) error {
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
	for i, cm := range cfg.ConfigMaps {
		if cm.Inject != nil {
			containerName := cm.Inject.Container
			if containerName == "" {
				containerName = defaultTargetContainer(pool)
			}
			if err := record(containerName, cm.Inject.MountPath, fmt.Sprintf("configEnv.configMaps[%d]", i)); err != nil {
				return err
			}
		}
	}
	for i, secret := range cfg.Secrets {
		if secret.Inject != nil && secret.Inject.Volume != nil {
			containerName := secret.Inject.Volume.Container
			if containerName == "" {
				containerName = defaultTargetContainer(pool)
			}
			if err := record(containerName, secret.Inject.Volume.MountPath, fmt.Sprintf("configEnv.secrets[%d]", i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func defaultTargetContainer(pool *arlv1alpha1.WarmPool) string {
	for _, c := range pool.Spec.Template.Spec.Containers {
		if c.Name == "executor" {
			return c.Name
		}
	}
	for _, c := range pool.Spec.Template.Spec.Containers {
		if c.Name != "sidecar" {
			return c.Name
		}
	}
	return ""
}

func renderConfigEnvForValidation(cfg *arlv1alpha1.ConfigEnvSpec) (*arlv1alpha1.ConfigEnvSpec, error) {
	return configenvutil.RenderSpec(cfg)
}

func validateSecretEnvVars(secretIndex int, envVars []arlv1alpha1.SecretEnvVar) error {
	seenNames := make(map[string]struct{}, len(envVars))
	for i, envVar := range envVars {
		if envVar.Name == "" {
			return fmt.Errorf("configEnv.secrets[%d].inject.asEnv[%d].name is required", secretIndex, i)
		}
		if errs := validation.IsEnvVarName(envVar.Name); len(errs) > 0 {
			return fmt.Errorf("configEnv.secrets[%d].inject.asEnv[%d].name: %s", secretIndex, i, errs[0])
		}
		if envVar.Key == "" {
			return fmt.Errorf("configEnv.secrets[%d].inject.asEnv[%d].key is required", secretIndex, i)
		}
		if errs := validation.IsConfigMapKey(envVar.Key); len(errs) > 0 {
			return fmt.Errorf("configEnv.secrets[%d].inject.asEnv[%d].key: %s", secretIndex, i, errs[0])
		}
		if _, ok := seenNames[envVar.Name]; ok {
			return fmt.Errorf("configEnv.secrets[%d].inject.asEnv[%d].name duplicates another env var name %q", secretIndex, i, envVar.Name)
		}
		seenNames[envVar.Name] = struct{}{}
	}
	return nil
}
