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

var expCreateCmd = &cobra.Command{
	Use:   "create <experiment-id>",
	Short: "Create an experiment with managed sessions",
	Long:  "Creates one or more managed sessions under an experiment ID. The sandbox-backed pool is auto-created.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		image, _ := cmd.Flags().GetString("image")
		profile, _ := cmd.Flags().GetString("profile")
		count, _ := cmd.Flags().GetInt("sessions")

		if image == "" {
			return fmt.Errorf("--image is required")
		}

		c := newClient()
		var sessions []ManagedSessionInfo

		for i := 0; i < count; i++ {
			info, err := c.CreateManagedSession(CreateManagedSessionRequest{
				Image:        image,
				Profile:      profile,
				ExperimentID: args[0],
				Namespace:    flagNamespace,
			})
			if err != nil {
				fmt.Fprintf(os.Stderr, "Session %d/%d failed: %v\n", i+1, count, err)
				continue
			}
			sessions = append(sessions, *info)
		}

		if flagOutput == "json" {
			printJSON(sessions)
			return nil
		}

		if len(sessions) == 0 {
			return fmt.Errorf("no sessions created")
		}

		fmt.Printf("Experiment %s: created %d session(s), profile=%s\n", args[0], len(sessions), sessions[0].Profile)
		w := newTabWriter()
		fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", s.ID, s.Profile, shortImage(s.Image), s.PodName)
		}
		return w.Flush()
	},
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
		fmt.Fprintln(w, "EXPERIMENT\tSESSIONS\tPROFILE\tIMAGE\tNAMESPACE")
		for _, e := range exps {
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", e.ExperimentID, e.SessionCount, e.Profile, shortImage(e.Image), e.Namespace)
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
		fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD\tAGE")
		for _, s := range sessions {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Profile, shortImage(s.Image), s.PodName, age(s.CreatedAt))
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
				stats["image"] = sessions[0].Image
				stats["profile"] = sessions[0].Profile
				stats["namespace"] = sessions[0].Namespace
			}
			printJSON(stats)
			return nil
		}

		fmt.Printf("Experiment:  %s\n", args[0])
		fmt.Printf("Sessions:    %d\n", len(sessions))
		if len(sessions) > 0 {
			fmt.Printf("Image:       %s\n", sessions[0].Image)
			fmt.Printf("Profile:     %s\n", sessions[0].Profile)
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
	expCreateCmd.Flags().String("image", "", "Container image (required)")
	expCreateCmd.Flags().String("profile", "default", "Resource profile")
	expCreateCmd.Flags().Int("sessions", 1, "Number of sessions to create")

	expDeleteCmd.Flags().Bool("force", false, "Skip confirmation")

	expCmd.AddCommand(expCreateCmd)
	expCmd.AddCommand(expListCmd)
	expCmd.AddCommand(expSessionsCmd)
	expCmd.AddCommand(expStatsCmd)
	expCmd.AddCommand(expDeleteCmd)
}
