package gateway

import (
	"fmt"
	"strings"
)

// injectDevboxEnv appends devbox-specific environment variables (SSH keys,
// git config) to the claim env var list so they are available inside the
// sandbox container.
func injectDevboxEnv(claimEnv []RuntimeEnvVar, cfg *DevboxConfig) []RuntimeEnvVar {
	if cfg == nil {
		return claimEnv
	}
	if len(cfg.SSHPublicKeys) > 0 {
		claimEnv = append(claimEnv, RuntimeEnvVar{
			Name:  "ARL_DEVBOX_SSH_PUBLIC_KEYS",
			Value: strings.Join(cfg.SSHPublicKeys, "\n"),
		})
	}
	if cfg.GitConfig != nil {
		if cfg.GitConfig.Name != "" {
			claimEnv = append(claimEnv, RuntimeEnvVar{
				Name:  "ARL_DEVBOX_GIT_USER_NAME",
				Value: cfg.GitConfig.Name,
			})
		}
		if cfg.GitConfig.Email != "" {
			claimEnv = append(claimEnv, RuntimeEnvVar{
				Name:  "ARL_DEVBOX_GIT_USER_EMAIL",
				Value: cfg.GitConfig.Email,
			})
		}
	}
	return claimEnv
}

// buildConnectionInfo constructs connection info for a devbox session.
func buildConnectionInfo(sessionID, podIP string, cfg *DevboxConfig) *ConnectionInfo {
	info := &ConnectionInfo{
		Shell: "/v1/sessions/" + sessionID + "/shell",
	}
	if cfg == nil {
		return info
	}
	hasSSH := false
	for _, p := range cfg.Ports {
		proto := strings.ToLower(p.Protocol)
		if proto == "" {
			proto = "tcp"
		}
		name := p.Name
		if name == "" {
			name = fmt.Sprintf("port-%d", p.Port)
		}
		info.Ports = append(info.Ports, PortInfo{
			Name:          name,
			ContainerPort: p.Port,
			Protocol:      proto,
		})
		if p.Port == 22 {
			hasSSH = true
		}
	}
	if len(cfg.SSHPublicKeys) > 0 && !hasSSH {
		info.Ports = append(info.Ports, PortInfo{
			Name:          "ssh",
			ContainerPort: 22,
			Protocol:      "tcp",
		})
	}
	if len(cfg.SSHPublicKeys) > 0 {
		info.SSH = &SSHInfo{Host: podIP, Port: 22}
	}
	return info
}

func devboxVolumeClaimTemplates(req CreateSessionRequest) []RuntimeVolumeClaimTemplate {
	if req.Mode != SessionModeDevbox || req.Devbox == nil {
		return nil
	}
	storageSize := strings.TrimSpace(req.Devbox.StorageSize)
	if storageSize == "" {
		return nil
	}
	return []RuntimeVolumeClaimTemplate{{
		Name:        "workspace",
		StorageSize: storageSize,
		AccessMode:  "ReadWriteOnce",
	}}
}
