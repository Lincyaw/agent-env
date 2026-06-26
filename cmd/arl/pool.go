package main

import (
	"fmt"
	"os"
	"strings"

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
		allNamespaces, _ := cmd.Flags().GetBool("all-namespaces")
		ns := flagNamespace
		if allNamespaces {
			ns = ""
		}

		c := newClient()
		pools, err := c.ListPools(ns)
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
			fmt.Fprintln(w, "NAME\tNAMESPACE\tIMAGE\tREPLICAS\tREADY\tALLOCATED\tSTATUS\tAGE")
			for _, p := range pools {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
					p.Name, p.Namespace, shortImage(p.Image),
					p.Replicas, p.ReadyReplicas, p.AllocatedReplicas,
					conditionSummary(p.Conditions), age(p.CreatedAt))
			}
		} else {
			fmt.Fprintln(w, "NAME\tREPLICAS\tREADY\tALLOCATED\tSTATUS\tAGE")
			for _, p := range pools {
				fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%s\t%s\n",
					p.Name, p.Replicas, p.ReadyReplicas, p.AllocatedReplicas,
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
		p, err := c.GetPool(args[0], flagNamespace)
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(p)
			return nil
		}

		fmt.Printf("Name:       %s\n", p.Name)
		fmt.Printf("Namespace:  %s\n", p.Namespace)
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
		replicas, _ := cmd.Flags().GetInt32("replicas")

		if image == "" {
			return fmt.Errorf("--image is required")
		}

		c := newClient()
		if err := c.CreatePool(CreatePoolRequest{
			Name:      args[0],
			Image:     image,
			Replicas:  replicas,
			Namespace: flagNamespace,
		}); err != nil {
			return err
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

		c := newClient()
		p, err := c.ScalePool(args[0], ScalePoolRequest{
			Replicas:  replicas,
			Namespace: flagNamespace,
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(p)
			return nil
		}

		fmt.Printf("Pool %s scaled to %d replicas (ready=%d).\n", args[0], p.Replicas, p.ReadyReplicas)
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
			fmt.Fprintf(os.Stderr, "Delete pool %q in namespace %q? Use --force to confirm.\n", args[0], flagNamespace)
			return fmt.Errorf("aborted (use --force)")
		}

		c := newClient()
		if err := c.DeletePool(args[0], flagNamespace); err != nil {
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
			return fmt.Errorf("no command specified; use: arl pool exec <pool> -- <cmd>")
		}

		c := newClient()

		// Create a temporary session from the pool
		var sessInfo SessionInfo
		if err := c.do("POST", "/v1/sessions", map[string]string{
			"poolRef":   poolName,
			"namespace": flagNamespace,
		}, &sessInfo); err != nil {
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
	poolListCmd.Flags().BoolP("all-namespaces", "A", false, "List pools across all namespaces")

	poolCreateCmd.Flags().String("image", "", "Container image (required)")
	poolCreateCmd.Flags().Int32("replicas", 2, "Number of warm replicas")

	poolScaleCmd.Flags().Int32("replicas", 0, "Target replica count")
	poolScaleCmd.MarkFlagRequired("replicas")

	poolDeleteCmd.Flags().Bool("force", false, "Skip confirmation")

	poolCmd.AddCommand(poolListCmd)
	poolCmd.AddCommand(poolGetCmd)
	poolCmd.AddCommand(poolCreateCmd)
	poolCmd.AddCommand(poolScaleCmd)
	poolLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	poolLogsCmd.Flags().Int("tail", 100, "Number of recent lines to show")

	poolCmd.AddCommand(poolDeleteCmd)
	poolCmd.AddCommand(poolExecCmd)
	poolCmd.AddCommand(poolLogsCmd)
}
