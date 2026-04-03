package configenv

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	arlv1alpha1 "github.com/Lincyaw/agent-env/api/v1alpha1"
)

const (
	HashAnnotation     = "arl.infra.io/config-env-hash"
	LegacyAnnotation   = "arl.infra.io/config-env"
	ReadyConditionType = "ConfigEnvReady"
)

func ResolveSpec(pool *arlv1alpha1.WarmPool) (*arlv1alpha1.ConfigEnvSpec, error) {
	if pool.Spec.ConfigEnv != nil {
		return pool.Spec.ConfigEnv.DeepCopy(), nil
	}

	raw := strings.TrimSpace(pool.Annotations[LegacyAnnotation])
	if raw == "" {
		return nil, nil
	}

	var cfg arlv1alpha1.ConfigEnvSpec
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("parse legacy configEnv annotation: %w", err)
	}
	return &cfg, nil
}

func RenderSpec(cfg *arlv1alpha1.ConfigEnvSpec) (*arlv1alpha1.ConfigEnvSpec, error) {
	if cfg == nil {
		return nil, nil
	}

	rendered := cfg.DeepCopy()
	ctx := map[string]any{"Vars": rendered.Vars}

	for i := range rendered.ConfigMaps {
		rendered.ConfigMaps[i].Name = mustDefault(rendered.ConfigMaps[i].Name, fmt.Sprintf("configmap-%d", i))
		name, err := renderTemplateString(rendered.ConfigMaps[i].Name, ctx)
		if err != nil {
			return nil, fmt.Errorf("render configMap[%d].metadata.name: %w", i, err)
		}
		rendered.ConfigMaps[i].Name = name
		if rendered.ConfigMaps[i].Namespace != "" {
			ns, err := renderTemplateString(rendered.ConfigMaps[i].Namespace, ctx)
			if err != nil {
				return nil, fmt.Errorf("render configMap[%d].metadata.namespace: %w", i, err)
			}
			rendered.ConfigMaps[i].Namespace = ns
		}
		for key, value := range rendered.ConfigMaps[i].Data {
			out, err := renderTemplateString(value, ctx)
			if err != nil {
				return nil, fmt.Errorf("render configMap[%d].data[%q]: %w", i, key, err)
			}
			rendered.ConfigMaps[i].Data[key] = out
		}
		if rendered.ConfigMaps[i].Inject != nil {
			injection := rendered.ConfigMaps[i].Inject
			if injection.Container != "" {
				out, err := renderTemplateString(injection.Container, ctx)
				if err != nil {
					return nil, fmt.Errorf("render configMap[%d].inject.container: %w", i, err)
				}
				injection.Container = out
			}
			out, err := renderTemplateString(injection.MountPath, ctx)
			if err != nil {
				return nil, fmt.Errorf("render configMap[%d].inject.mountPath: %w", i, err)
			}
			injection.MountPath = out
			if injection.SubPath != "" {
				out, err = renderTemplateString(injection.SubPath, ctx)
				if err != nil {
					return nil, fmt.Errorf("render configMap[%d].inject.subPath: %w", i, err)
				}
				injection.SubPath = out
			}
		}
	}

	for i := range rendered.Secrets {
		rendered.Secrets[i].Name = mustDefault(rendered.Secrets[i].Name, fmt.Sprintf("secret-%d", i))
		name, err := renderTemplateString(rendered.Secrets[i].Name, ctx)
		if err != nil {
			return nil, fmt.Errorf("render secret[%d].metadata.name: %w", i, err)
		}
		rendered.Secrets[i].Name = name
		if rendered.Secrets[i].Namespace != "" {
			ns, err := renderTemplateString(rendered.Secrets[i].Namespace, ctx)
			if err != nil {
				return nil, fmt.Errorf("render secret[%d].metadata.namespace: %w", i, err)
			}
			rendered.Secrets[i].Namespace = ns
		}
		for key, value := range rendered.Secrets[i].StringData {
			out, err := renderTemplateString(value, ctx)
			if err != nil {
				return nil, fmt.Errorf("render secret[%d].stringData[%q]: %w", i, key, err)
			}
			rendered.Secrets[i].StringData[key] = out
		}
		if rendered.Secrets[i].Inject != nil {
			if rendered.Secrets[i].Inject.Volume != nil {
				injection := rendered.Secrets[i].Inject.Volume
				if injection.Container != "" {
					out, err := renderTemplateString(injection.Container, ctx)
					if err != nil {
						return nil, fmt.Errorf("render secret[%d].inject.volume.container: %w", i, err)
					}
					injection.Container = out
				}
				out, err := renderTemplateString(injection.MountPath, ctx)
				if err != nil {
					return nil, fmt.Errorf("render secret[%d].inject.volume.mountPath: %w", i, err)
				}
				injection.MountPath = out
				if injection.SubPath != "" {
					out, err = renderTemplateString(injection.SubPath, ctx)
					if err != nil {
						return nil, fmt.Errorf("render secret[%d].inject.volume.subPath: %w", i, err)
					}
					injection.SubPath = out
				}
			}
			for j := range rendered.Secrets[i].Inject.AsEnv {
				nameOut, err := renderTemplateString(rendered.Secrets[i].Inject.AsEnv[j].Name, ctx)
				if err != nil {
					return nil, fmt.Errorf("render secret[%d].inject.asEnv[%d].name: %w", i, j, err)
				}
				rendered.Secrets[i].Inject.AsEnv[j].Name = nameOut
				keyOut, err := renderTemplateString(rendered.Secrets[i].Inject.AsEnv[j].Key, ctx)
				if err != nil {
					return nil, fmt.Errorf("render secret[%d].inject.asEnv[%d].key: %w", i, j, err)
				}
				rendered.Secrets[i].Inject.AsEnv[j].Key = keyOut
			}
		}
	}

	for i := range rendered.EnvVars {
		name, err := renderTemplateString(rendered.EnvVars[i].Name, ctx)
		if err != nil {
			return nil, fmt.Errorf("render envVars[%d].name: %w", i, err)
		}
		rendered.EnvVars[i].Name = name
		if rendered.EnvVars[i].Value != "" {
			value, err := renderTemplateString(rendered.EnvVars[i].Value, ctx)
			if err != nil {
				return nil, fmt.Errorf("render envVars[%d].value: %w", i, err)
			}
			rendered.EnvVars[i].Value = value
		}
	}

	return rendered, nil
}

func DesiredHashForPool(pool *arlv1alpha1.WarmPool) (string, error) {
	cfg, err := ResolveSpec(pool)
	if err != nil || cfg == nil {
		return "", err
	}

	rendered, err := RenderSpec(cfg)
	if err != nil {
		return "", err
	}
	return HashSpec(rendered), nil
}

func HashSpec(cfg *arlv1alpha1.ConfigEnvSpec) string {
	if cfg == nil {
		return ""
	}
	raw, _ := json.Marshal(cfg)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:8])
}

func renderTemplateString(value string, data any) (string, error) {
	if value == "" || !strings.Contains(value, "{{") {
		return value, nil
	}
	tpl, err := template.New("configenv").Option("missingkey=error").Parse(value)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func mustDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
