package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var devboxCmd = &cobra.Command{
	Use:   "devbox",
	Short: "Manage devbox sessions",
}

var devboxCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a devbox session",
	RunE: func(cmd *cobra.Command, args []string) error {
		image, _ := cmd.Flags().GetString("image")
		profile, _ := cmd.Flags().GetString("profile")
		idleTimeout, _ := cmd.Flags().GetInt("idle-timeout")
		waitTimeout, _ := cmd.Flags().GetDuration("wait-timeout")
		allocationTimeout, err := allocationTimeoutSecondsFromDuration(waitTimeout)
		if err != nil {
			return err
		}
		sshKeys, _ := cmd.Flags().GetStringArray("ssh-key")
		sshKeyFiles, _ := cmd.Flags().GetStringArray("ssh-key-file")
		gitName, _ := cmd.Flags().GetString("git-name")
		gitEmail, _ := cmd.Flags().GetString("git-email")
		storageSize, _ := cmd.Flags().GetString("storage")
		ports, _ := cmd.Flags().GetInt32Slice("port")
		privateContainers, err := privateContainersFromFlags(cmd)
		if err != nil {
			return err
		}

		if image == "" && profile == "" {
			return usageError("--image or --profile is required")
		}

		allKeys, err := collectSSHKeys(sshKeys, sshKeyFiles)
		if err != nil {
			return err
		}

		devbox := &DevboxConfig{
			StorageSize: storageSize,
		}
		if len(allKeys) > 0 {
			devbox.SSHPublicKeys = allKeys
		}
		if gitName != "" || gitEmail != "" {
			devbox.GitConfig = &GitConfig{Name: gitName, Email: gitEmail}
		}
		for _, p := range ports {
			devbox.Ports = append(devbox.Ports, DevboxPort{Port: p})
		}

		c := newClient()
		s, err := c.CreateSession(CreateSessionRequest{
			Image:                    image,
			Profile:                  profile,
			Mode:                     "devbox",
			Devbox:                   devbox,
			IdleTimeoutSeconds:       idleTimeout,
			AllocationTimeoutSeconds: allocationTimeout,
			PrivateContainers:        privateContainers,
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(s)
			return nil
		}

		fmt.Printf("Devbox %s created.\n", s.ID)
		fmt.Printf("Image:      %s\n", s.Image)
		fmt.Printf("Profile:    %s\n", s.Profile)
		fmt.Printf("Pod:        %s\n", s.PodName)
		fmt.Printf("Pod IP:     %s\n", s.PodIP)
		if s.ConnectionInfo != nil {
			if s.ConnectionInfo.SSH != nil {
				fmt.Printf("SSH:        ssh root@%s -p %d\n", s.ConnectionInfo.SSH.Host, s.ConnectionInfo.SSH.Port)
			}
			for _, p := range s.ConnectionInfo.Ports {
				fmt.Printf("Port:       %s %d/%s\n", p.Name, p.ContainerPort, p.Protocol)
			}
		}
		return nil
	},
}

func collectSSHKeys(literal []string, files []string) ([]string, error) {
	var keys []string
	for _, k := range literal {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read ssh key file %q: %w", f, err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				keys = append(keys, line)
			}
		}
	}
	return keys, nil
}

func init() {
	devboxCreateCmd.Flags().String("image", "", "Container image")
	devboxCreateCmd.Flags().String("profile", "default", "Resource profile")
	devboxCreateCmd.Flags().Int("idle-timeout", 0, "Idle timeout in seconds (0 uses gateway default)")
	devboxCreateCmd.Flags().Duration("wait-timeout", 0, "Maximum time to wait for session allocation (0 waits until ready or cancellation)")
	devboxCreateCmd.Flags().StringArray("ssh-key", nil, "SSH public key (repeatable)")
	devboxCreateCmd.Flags().StringArray("ssh-key-file", nil, "Read SSH public keys from file (repeatable)")
	devboxCreateCmd.Flags().String("git-name", "", "Git user.name inside the devbox")
	devboxCreateCmd.Flags().String("git-email", "", "Git user.email inside the devbox")
	devboxCreateCmd.Flags().String("storage", "", "Persistent storage size (e.g. 10Gi)")
	devboxCreateCmd.Flags().Int32Slice("port", nil, "Ports to expose (repeatable, e.g. --port 8080 --port 3000)")
	addPrivateContainerFlags(devboxCreateCmd)

	devboxCmd.AddCommand(devboxCreateCmd)
}
