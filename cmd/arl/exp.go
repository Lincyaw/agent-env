package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var expCmd = &cobra.Command{
	Use:     "exp",
	Aliases: []string{"experiment"},
	Short:   "Manage experiments",
}

var expListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all experiments",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		exps, err := c.ListExperiments()
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(exps)
			return nil
		}

		if len(exps) == 0 {
			fmt.Println("No experiments found.")
			return nil
		}

		w := newTabWriter()
		fmt.Fprintln(w, "EXPERIMENT\tSESSIONS\tPOOL\tNAMESPACE")
		for _, e := range exps {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", e.ExperimentID, e.SessionCount, e.PoolRef, e.Namespace)
		}
		return w.Flush()
	},
}

var expSessionsCmd = &cobra.Command{
	Use:   "sessions <experiment-id>",
	Short: "List sessions for an experiment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		sessions, err := c.ListExperimentSessions(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(sessions)
			return nil
		}

		if len(sessions) == 0 {
			fmt.Printf("No sessions found for experiment %s.\n", args[0])
			return nil
		}

		w := newTabWriter()
		fmt.Fprintln(w, "ID\tPOOL\tPOD\tAGE")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.PoolRef, s.PodName, age(s.CreatedAt))
		}
		return w.Flush()
	},
}

var expStatsCmd = &cobra.Command{
	Use:   "stats <experiment-id>",
	Short: "Show experiment statistics",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		sessions, err := c.ListExperimentSessions(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			stats := map[string]any{
				"experimentId": args[0],
				"sessions":     len(sessions),
			}
			if len(sessions) > 0 {
				stats["poolRef"] = sessions[0].PoolRef
				stats["namespace"] = sessions[0].Namespace
			}
			printJSON(stats)
			return nil
		}

		fmt.Printf("Experiment:  %s\n", args[0])
		fmt.Printf("Sessions:    %d\n", len(sessions))
		if len(sessions) > 0 {
			fmt.Printf("Pool:        %s\n", sessions[0].PoolRef)
			fmt.Printf("Namespace:   %s\n", sessions[0].Namespace)
		}
		return nil
	},
}

var expDeleteCmd = &cobra.Command{
	Use:   "delete <experiment-id>",
	Short: "Delete all sessions for an experiment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			fmt.Fprintf(os.Stderr, "Delete all sessions for experiment %q? Use --force to confirm.\n", args[0])
			return fmt.Errorf("aborted (use --force)")
		}

		c := newClient()
		resp, err := c.DeleteExperiment(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		deleted, _ := resp["deleted"].(float64)
		fmt.Printf("Deleted %d sessions for experiment %s.\n", int(deleted), args[0])
		return nil
	},
}

func init() {
	expDeleteCmd.Flags().Bool("force", false, "Skip confirmation")

	expCmd.AddCommand(expListCmd)
	expCmd.AddCommand(expSessionsCmd)
	expCmd.AddCommand(expStatsCmd)
	expCmd.AddCommand(expDeleteCmd)
}
