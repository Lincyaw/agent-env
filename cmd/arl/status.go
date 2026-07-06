package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show gateway health and summary",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := newClient()

		healthy := true
		if err := c.HealthCheck(); err != nil {
			healthy = false
			if flagOutput != "json" {
				fmt.Printf("Gateway:     UNHEALTHY (%v)\n", err)
			}
		}

		summary, summaryErr := c.Summary()

		if flagOutput == "json" {
			result := map[string]any{
				"healthy": healthy,
				"gateway": flagGatewayURL,
			}
			if summaryErr != nil {
				result["summaryError"] = summaryErr.Error()
			} else {
				result["sessions"] = summary.Sessions
				result["managedSessions"] = summary.ManagedSessions
				result["pools"] = summary.Pools
				result["readyReplicas"] = summary.ReadyReplicas
				result["allocatedReplicas"] = summary.AllocatedReplicas
				result["experiments"] = summary.Experiments
			}
			printJSON(result)
			return nil
		}

		if healthy {
			fmt.Printf("Gateway:       OK (%s)\n", flagGatewayURL)
		}

		if summaryErr != nil {
			fmt.Printf("Summary:       error (%v)\n", summaryErr)
		} else {
			fmt.Printf("Sessions:      %d total (%d managed)\n", summary.Sessions, summary.ManagedSessions)
			fmt.Printf("Pools:         %d (ready=%d, allocated=%d)\n", summary.Pools, summary.ReadyReplicas, summary.AllocatedReplicas)
			fmt.Printf("Experiments:   %d\n", summary.Experiments)
		}

		return nil
	},
}
