package gateway

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	executorContainerName = "executor"
)

func validatePrivateContainers(containers []PrivateContainerSpec) error {
	seen := make(map[string]struct{}, len(containers))
	for i, container := range containers {
		name := strings.TrimSpace(container.Name)
		if name == "" {
			return fmt.Errorf("privateContainers[%d].name is required", i)
		}
		if container.Name != name {
			return fmt.Errorf("privateContainers[%d].name must not contain leading or trailing whitespace", i)
		}
		if name == executorContainerName {
			return fmt.Errorf("private container name %q is reserved", name)
		}
		if errs := validation.IsDNS1123Label(name); len(errs) > 0 {
			return fmt.Errorf("private container name %q is invalid: %s", name, strings.Join(errs, "; "))
		}
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate private container name %q", name)
		}
		seen[name] = struct{}{}
		image := strings.TrimSpace(container.Image)
		if image == "" {
			return fmt.Errorf("privateContainers[%d].image is required", i)
		}
		if container.Image != image {
			return fmt.Errorf("privateContainers[%d].image must not contain leading or trailing whitespace", i)
		}
		if container.MountWorkspace {
			mountPath := strings.TrimSpace(container.WorkspaceMountPath)
			if mountPath != "" && !strings.HasPrefix(mountPath, "/") {
				return fmt.Errorf("privateContainers[%d].workspaceMountPath must be absolute", i)
			}
		}
		access := strings.TrimSpace(container.WorkspaceAccess)
		if access != "" && !strings.EqualFold(access, "readWrite") && !strings.EqualFold(access, "readOnly") {
			return fmt.Errorf("privateContainers[%d].workspaceAccess must be readWrite or readOnly", i)
		}
	}
	return nil
}

func privateContainerMap(containers []PrivateContainerSpec) map[string]PrivateContainerSpec {
	if len(containers) == 0 {
		return nil
	}
	out := make(map[string]PrivateContainerSpec, len(containers))
	for _, container := range containers {
		out[container.Name] = container
	}
	return out
}
