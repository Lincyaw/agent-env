package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:     "session",
	Aliases: []string{"sess"},
	Short:   "Manage sessions",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		filterProfile, _ := cmd.Flags().GetString("profile")
		filterExp, _ := cmd.Flags().GetString("experiment")

		c := newClient()
		sessions, err := c.ListSessions()
		if err != nil {
			return err
		}

		var filtered []SessionListItem
		for _, s := range sessions {
			if filterProfile != "" && s.Profile != filterProfile {
				continue
			}
			if filterExp != "" && s.ExperimentID != filterExp {
				continue
			}
			filtered = append(filtered, s)
		}

		if flagOutput == "json" {
			printJSON(filtered)
			return nil
		}

		if len(filtered) == 0 {
			fmt.Println("No sessions found.")
			return nil
		}

		w := newTabWriter()
		if flagOutput == "wide" {
			fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD\tPOD-IP\tNAMESPACE\tEXPERIMENT\tAGE")
			for _, s := range filtered {
				exp := s.ExperimentID
				if exp == "" {
					exp = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					s.ID, s.Profile, shortImage(s.Image), s.PodName, s.PodIP, s.Namespace, exp, age(s.CreatedAt))
			}
		} else {
			fmt.Fprintln(w, "ID\tPROFILE\tIMAGE\tPOD\tEXPERIMENT\tAGE")
			for _, s := range filtered {
				exp := s.ExperimentID
				if exp == "" {
					exp = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					s.ID, s.Profile, shortImage(s.Image), s.PodName, exp, age(s.CreatedAt))
			}
		}
		return w.Flush()
	},
}

var sessionGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get session details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		s, err := c.GetSession(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(s)
			return nil
		}

		fmt.Printf("ID:         %s\n", s.ID)
		fmt.Printf("Sandbox:    %s\n", s.SandboxName)
		fmt.Printf("Namespace:  %s\n", s.Namespace)
		fmt.Printf("Image:      %s\n", s.Image)
		fmt.Printf("Profile:    %s\n", s.Profile)
		fmt.Printf("Pod:        %s\n", s.PodName)
		fmt.Printf("Pod IP:     %s\n", s.PodIP)
		fmt.Printf("Age:        %s\n", age(s.CreatedAt))
		return nil
	},
}

var sessionDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		if err := c.DeleteSession(args[0]); err != nil {
			return err
		}
		fmt.Printf("Session %s deleted.\n", args[0])
		return nil
	},
}

var sessionHistoryCmd = &cobra.Command{
	Use:   "history <id>",
	Short: "Show execution history",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()
		records, err := c.GetHistory(args[0])
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(records)
			return nil
		}

		if len(records) == 0 {
			fmt.Println("No history.")
			return nil
		}

		w := newTabWriter()
		fmt.Fprintln(w, "STEP\tNAME\tEXIT\tDURATION\tTIME")
		for _, r := range records {
			fmt.Fprintf(w, "%d\t%s\t%d\t%dms\t%s\n",
				r.Index, r.Name, r.Output.ExitCode, r.DurationMs,
				r.Timestamp.Format("15:04:05"))
		}
		w.Flush()

		verbose, _ := cmd.Flags().GetBool("verbose")
		if verbose {
			fmt.Println()
			for _, r := range records {
				fmt.Printf("--- step %d: %s ---\n", r.Index, r.Name)
				if r.Output.Stdout != "" {
					fmt.Println(r.Output.Stdout)
				}
				if r.Output.Stderr != "" {
					fmt.Fprintf(os.Stderr, "%s\n", r.Output.Stderr)
				}
			}
		}
		return nil
	},
}

var sessionTrajectoryCmd = &cobra.Command{
	Use:   "trajectory <id>",
	Short: "Export JSONL trajectory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		outFile, _ := cmd.Flags().GetString("file")

		c := newClient()
		data, err := c.GetTrajectory(args[0])
		if err != nil {
			return err
		}

		if outFile != "" {
			if err := os.WriteFile(outFile, data, 0644); err != nil {
				return fmt.Errorf("write file: %w", err)
			}
			fmt.Printf("Trajectory written to %s (%d bytes).\n", outFile, len(data))
			return nil
		}

		os.Stdout.Write(data)
		return nil
	},
}

var sessionExecCmd = &cobra.Command{
	Use:   "exec <id> -- <command...>",
	Short: "Execute a command in a session",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := args[0]
		cmdArgs := args[1:]

		if dash := cmd.ArgsLenAtDash(); dash >= 0 {
			cmdArgs = args[dash:]
		}

		if len(cmdArgs) == 0 {
			return fmt.Errorf("no command specified; use: arl session exec <id> -- <cmd>")
		}

		c := newClient()
		resp, err := c.Execute(sessionID, ExecuteRequest{
			Steps: []StepRequest{
				{
					Name:    strings.Join(cmdArgs, " "),
					Command: cmdArgs,
				},
			},
		})
		if err != nil {
			return err
		}

		if flagOutput == "json" {
			printJSON(resp)
			return nil
		}

		return printExecResults(resp.Results)
	},
}

var sessionShellCmd = &cobra.Command{
	Use:   "shell <id>",
	Short: "Open interactive shell (WebSocket)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runShell(args[0])
	},
}

var sessionLogsCmd = &cobra.Command{
	Use:   "logs <id>",
	Short: "Stream session pod logs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		follow, _ := cmd.Flags().GetBool("follow")
		tail, _ := cmd.Flags().GetInt("tail")

		c := newClient()
		return streamLogs(c, "/v1/sessions/"+args[0]+"/logs", follow, tail, false)
	},
}

func init() {
	sessionListCmd.Flags().String("profile", "", "Filter by profile")
	sessionListCmd.Flags().String("experiment", "", "Filter by experiment ID")

	sessionHistoryCmd.Flags().BoolP("verbose", "v", false, "Show step output")

	sessionTrajectoryCmd.Flags().StringP("file", "f", "", "Write trajectory to file instead of stdout")

	sessionLogsCmd.Flags().BoolP("follow", "f", false, "Follow log output")
	sessionLogsCmd.Flags().Int("tail", 100, "Number of recent lines to show")

	sessionCmd.AddCommand(sessionListCmd)
	sessionCmd.AddCommand(sessionGetCmd)
	sessionCmd.AddCommand(sessionDeleteCmd)
	sessionCmd.AddCommand(sessionHistoryCmd)
	sessionCmd.AddCommand(sessionTrajectoryCmd)
	sessionCmd.AddCommand(sessionExecCmd)
	sessionCmd.AddCommand(sessionShellCmd)
	sessionCmd.AddCommand(sessionLogsCmd)
}
