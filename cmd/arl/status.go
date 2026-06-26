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

		sessions, sessErr := c.ListSessions()
		pools, poolErr := c.ListPools("")
		exps, expErr := c.ListExperiments()

		if flagOutput == "json" {
			result := map[string]any{
				"healthy":  healthy,
				"gateway":  flagGatewayURL,
				"sessions": len(sessions),
				"pools":    len(pools),
			}
			if exps != nil {
				result["experiments"] = len(exps)
			}
			if sessErr != nil {
				result["sessionError"] = sessErr.Error()
			}
			if poolErr != nil {
				result["poolError"] = poolErr.Error()
			}
			printJSON(result)
			return nil
		}

		if healthy {
			fmt.Printf("Gateway:       OK (%s)\n", flagGatewayURL)
		}

		if sessErr != nil {
			fmt.Printf("Sessions:      error (%v)\n", sessErr)
		} else {
			managed := 0
			for _, s := range sessions {
				if s.Managed {
					managed++
				}
			}
			fmt.Printf("Sessions:      %d total (%d managed)\n", len(sessions), managed)
		}

		if poolErr != nil {
			fmt.Printf("Pools:         error (%v)\n", poolErr)
		} else {
			totalReady := int32(0)
			totalAlloc := int32(0)
			for _, p := range pools {
				totalReady += p.ReadyReplicas
				totalAlloc += p.AllocatedReplicas
			}
			fmt.Printf("Pools:         %d (ready=%d, allocated=%d)\n", len(pools), totalReady, totalAlloc)
		}

		if expErr != nil {
			fmt.Printf("Experiments:   error (%v)\n", expErr)
		} else {
			fmt.Printf("Experiments:   %d\n", len(exps))
		}

		return nil
	},
}
