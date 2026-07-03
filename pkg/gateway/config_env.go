package gateway

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

type configEnvPayload struct {
	Vars            map[string]string `json:"vars,omitempty"`
	EnvVars         []configEnvVar    `json:"envVars,omitempty"`
	EnvVarsSnake    []configEnvVar    `json:"env_vars,omitempty"`
	ConfigMaps      []json.RawMessage `json:"configMaps,omitempty"`
	ConfigMapsSnake []json.RawMessage `json:"config_maps,omitempty"`
	Secrets         []json.RawMessage `json:"secrets,omitempty"`
}

type configEnvVar struct {
	Name          string          `json:"name"`
	Value         string          `json:"value,omitempty"`
	ValueFrom     json.RawMessage `json:"valueFrom,omitempty"`
	ContainerName string          `json:"containerName,omitempty"`
}

func parseConfigEnvVars(raw json.RawMessage) ([]RuntimeEnvVar, error) {
	if !hasJSONPayload(raw) {
		return nil, nil
	}

	var payload configEnvPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode configEnv: %w", err)
	}
	if len(payload.ConfigMaps) > 0 || len(payload.ConfigMapsSnake) > 0 {
		return nil, fmt.Errorf("configEnv configMaps are not supported by SandboxClaim env injection yet")
	}
	if len(payload.Secrets) > 0 {
		return nil, fmt.Errorf("configEnv secrets are not supported by SandboxClaim env injection yet")
	}

	envVars := make([]RuntimeEnvVar, 0, len(payload.Vars)+len(payload.EnvVars)+len(payload.EnvVarsSnake))
	keys := make([]string, 0, len(payload.Vars))
	for key := range payload.Vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := validateConfigEnvVarName(key); err != nil {
			return nil, err
		}
		envVars = append(envVars, RuntimeEnvVar{Name: key, Value: payload.Vars[key]})
	}
	for _, envVar := range append(payload.EnvVars, payload.EnvVarsSnake...) {
		if err := validateConfigEnvVarName(envVar.Name); err != nil {
			return nil, err
		}
		if hasJSONPayload(envVar.ValueFrom) {
			return nil, fmt.Errorf("configEnv envVars valueFrom is not supported by SandboxClaim env injection")
		}
		envVars = append(envVars, RuntimeEnvVar{
			Name:          envVar.Name,
			Value:         envVar.Value,
			ContainerName: strings.TrimSpace(envVar.ContainerName),
		})
	}
	return envVars, nil
}

func validateConfigEnvVarName(name string) error {
	if errs := validation.IsEnvVarName(name); len(errs) > 0 {
		return fmt.Errorf("invalid configEnv env var name %q: %s", name, strings.Join(errs, "; "))
	}
	return nil
}
