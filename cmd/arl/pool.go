package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var poolCmd = &cobra.Command{
	Use:   "pool",
	Short: "Manage warm pools",
}

var poolListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all WarmPools",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		pools, err := c.ListPools()
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(pools)
			return nil
		}

		if len(pools) == 0 {
			fmt.Println("No pools found.")
			return nil
		}

		w := newTabWriter()
		if flagOutput == "wide" {
			fmt.Fprintln(w, "NAME\tPROFILE\tIMAGE\tREPLICAS\tREADY\tALLOCATED\tSTATUS\tAGE")
			for _, p := range pools {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
					p.Name, p.Profile, shortImage(p.Image),
					p.Replicas, p.ReadyReplicas, p.AllocatedReplicas,
					conditionSummary(p.Conditions), age(p.CreatedAt))
			}
		} else {
			fmt.Fprintln(w, "NAME\tPROFILE\tREPLICAS\tREADY\tALLOCATED\tSTATUS\tAGE")
			for _, p := range pools {
				fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
					p.Name, p.Profile, p.Replicas, p.ReadyReplicas, p.AllocatedReplicas,
					conditionSummary(p.Conditions), age(p.CreatedAt))
			}
		}
		return w.Flush()
	},
}

var poolGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get pool details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		p, err := c.GetPool(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(p)
			return nil
		}

		fmt.Printf("Name:       %s\n", p.Name)
		fmt.Printf("Profile:    %s\n", p.Profile)
		fmt.Printf("Image:      %s\n", p.Image)
		fmt.Printf("Replicas:   %d (ready=%d, allocated=%d)\n", p.Replicas, p.ReadyReplicas, p.AllocatedReplicas)
		fmt.Printf("Age:        %s\n", age(p.CreatedAt))
		if len(p.Conditions) > 0 {
			fmt.Println("Conditions:")
			w := newTabWriter()
			fmt.Fprintln(w, "  TYPE\tSTATUS\tREASON\tMESSAGE")
			for _, c := range p.Conditions {
				fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", c.Type, c.Status, c.Reason, truncate(c.Message, 60))
			}
			w.Flush()
		}
		return nil
	},
}

var poolCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a WarmPool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		image, _ := cmd.Flags().GetString("image")
		profile, _ := cmd.Flags().GetString("profile")
		replicas, _ := cmd.Flags().GetInt32("replicas")
		workspaceDir, _ := cmd.Flags().GetString("workspace-dir")
		wait, _ := cmd.Flags().GetBool("wait")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		minReady, _ := cmd.Flags().GetInt32("min-ready")
		privateContainers, err := privateContainersFromFlags(cmd)
		if err != nil {
			return err
		}

		if image == "" {
			return usageError("--image is required")
		}
		if profile == "" {
			profile = args[0]
		}

		c := newClient()
		if err := c.CreatePool(CreatePoolRequest{
			Name:              args[0],
			Image:             image,
			Profile:           profile,
			Replicas:          replicas,
			WorkspaceDir:      workspaceDir,
			PrivateContainers: privateContainers,
		}); err != nil {
			return err
		}

		if wait {
			if minReady < 0 {
				minReady = replicas
			}
			p, err := waitForPoolReady(c, args[0], minReady, timeout)
			if err != nil {
				return err
			}
			if flagOutput == "json" {
				printJSON(p)
				return nil
			}
			fmt.Printf("Pool %s ready (ready=%d/%d).\n", p.Name, p.ReadyReplicas, p.Replicas)
			return nil
		}

		if flagOutput == "json" {
			p, err := c.GetPool(args[0])
			if err != nil {
				return err
			}
			printJSON(p)
			return nil
		}

		fmt.Printf("Pool %s created.\n", args[0])
		return nil
	},
}

var poolScaleCmd = &cobra.Command{
	Use:   "scale <name>",
	Short: "Scale pool replicas",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		replicas, _ := cmd.Flags().GetInt32("replicas")
		wait, _ := cmd.Flags().GetBool("wait")
		timeout, _ := cmd.Flags().GetDuration("timeout")
		minReady, _ := cmd.Flags().GetInt32("min-ready")

		c := newClient()
		p, err := c.ScalePool(args[0], ScalePoolRequest{
			Replicas: replicas,
		})
		if err != nil {
			return err
		}
		if wait {
			if minReady < 0 {
				minReady = replicas
			}
			p, err = waitForPoolReady(c, args[0], minReady, timeout)
			if err != nil {
				return err
			}
		}

		if flagOutput == "json" {
			printJSON(p)
			return nil
		}

		fmt.Printf("Pool %s scaled to %d replicas (ready=%d).\n", args[0], p.Replicas, p.ReadyReplicas)
		return nil
	},
}

var poolWaitCmd = &cobra.Command{
	Use:   "wait <name>",
	Short: "Wait for a WarmPool to have ready capacity",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		minReady, _ := cmd.Flags().GetInt32("min-ready")
		timeout, _ := cmd.Flags().GetDuration("timeout")

		c := newClient()
		p, err := waitForPoolReady(c, args[0], minReady, timeout)
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(p)
			return nil
		}

		fmt.Printf("Pool %s ready (ready=%d/%d).\n", p.Name, p.ReadyReplicas, p.Replicas)
		return nil
	},
}

var poolDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a WarmPool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			fmt.Fprintf(os.Stderr, "Delete pool %q? Use --force to confirm.\n", args[0])
			return fmt.Errorf("aborted (use --force)")
		}

		c := newClient()
		if err := c.DeletePool(args[0]); err != nil {
			return err
		}

		fmt.Printf("Pool %s deleted.\n", args[0])
		return nil
	},
}

var poolExecCmd = &cobra.Command{
	Use:   "exec <pool-name> -- <command...>",
	Short: "Execute a command in a pool pod (creates a temporary session)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		poolName := args[0]
		cmdArgs := args[1:]
		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			cmdArgs = args[dash:]
		}
		if len(cmdArgs) == 0 {
			return usageError("no command specified; use: arl pool exec <pool> -- <cmd>")
		}

		c := newClient()
		pool, err := c.GetPool(poolName)
		if err != nil {
			return fmt.Errorf("get pool: %w", err)
		}
		profile := pool.Profile
		if profile == "" {
			profile = poolName
		}

		sessInfo, err := c.CreateSession(CreateSessionRequest{
			Image:   pool.Image,
			Profile: profile,
		})
		if err != nil {
			return fmt.Errorf("create temporary session: %w", err)
		}

		// Execute the command
		resp, execErr := c.Execute(sessInfo.ID, ExecuteRequest{
			Steps: []StepRequest{
				{
					Name:    strings.Join(cmdArgs, " "),
					Command: cmdArgs,
				},
			},
		})

		// Always clean up the session
		if delErr := c.DeleteSession(sessInfo.ID); delErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to delete temporary session %s: %v\n", sessInfo.ID, delErr)
		}

		if execErr != nil {
			return execErr
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		return printExecResults(resp.Results)
	},
}

func waitForPoolReady(c *Client, name string, minReady int32, timeout time.Duration) (*PoolInfo, error) {
	if timeout <= 0 {
		return nil, usageError("--timeout must be positive")
	}

	deadline := time.Now().Add(timeout)
	var last *PoolInfo
	var lastErr error
	for {
		p, err := c.GetPool(name)
		if err != nil {
			lastErr = err
		} else {
			last = p
			target := minReady
			if target < 0 {
				target = p.Replicas
			}
			if target < 0 {
				target = 0
			}
			if p.ReadyReplicas >= target {
				return p, nil
			}
		}

		if time.Now().After(deadline) {
			if last != nil {
				target := minReady
				if target < 0 {
					target = last.Replicas
				}
				return nil, fmt.Errorf("timeout waiting for pool %s ready: ready=%d want=%d replicas=%d", name, last.ReadyReplicas, target, last.Replicas)
			}
			return nil, fmt.Errorf("timeout waiting for pool %s ready: last error: %w", name, lastErr)
		}
		time.Sleep(2 * time.Second)
	}
}

var poolLogsCmd = &cobra.Command{
	Use:   "logs <name>",
	Short: "Stream aggregated pod logs for a pool",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")

		c := newClient()
		return streamLogs(c, "/v1/pools/"+args[0]+"/logs", follow, tail, true)
	},
}

func init() {
	poolCreateCmd.Flags().String("image", "", "Container image (required)")
	poolCreateCmd.Flags().String("profile", "", "Pool selection profile (default: pool name)")
	poolCreateCmd.Flags().Int32("replicas", 2, "Number of warm replicas")
	poolCreateCmd.Flags().String("workspace-dir", "", "Workspace directory inside each sandbox")
	poolCreateCmd.Flags().Bool("wait", false, "Wait until the pool has ready capacity")
	poolCreateCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait with --wait")
	poolCreateCmd.Flags().Int32("min-ready", -1, "Minimum ready sandboxes to wait for (-1 means desired replicas)")
	addPrivateContainerFlags(poolCreateCmd)

	poolScaleCmd.Flags().Int32("replicas", 0, "Target replica count")
	poolScaleCmd.MarkFlagRequired("replicas")
	poolScaleCmd.Flags().Bool("wait", false, "Wait until the scaled pool has ready capacity")
	poolScaleCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait with --wait")
	poolScaleCmd.Flags().Int32("min-ready", -1, "Minimum ready sandboxes to wait for (-1 means target replicas)")

	poolWaitCmd.Flags().Duration("timeout", 10*time.Minute, "Maximum time to wait")
	poolWaitCmd.Flags().Int32("min-ready", -1, "Minimum ready sandboxes to wait for (-1 means desired replicas)")

	poolDeleteCmd.Flags().Bool("force", false, "Skip confirmation")

	poolCmd.AddCommand(poolListCmd)
	poolCmd.AddCommand(poolGetCmd)
	poolCmd.AddCommand(poolCreateCmd)
	poolCmd.AddCommand(poolScaleCmd)
	poolCmd.AddCommand(poolWaitCmd)
	poolLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	poolLogsCmd.Flags().Int("tail", 100, "Number of recent lines to show")

	poolCmd.AddCommand(poolDeleteCmd)
	poolCmd.AddCommand(poolExecCmd)
	poolCmd.AddCommand(poolLogsCmd)
}
