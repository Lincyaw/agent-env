package main

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var metricsCmd = &cobra.Command{
	Use:   "metrics",
	Short: "Show Prometheus metrics from the gateway",
	Long:  "Fetches the /metrics endpoint and displays key ARL metrics. Use --filter to narrow output.",
	RunE: func(cmd *cobra.Command, args []string) error {
		filter, _ := cmd.Flags().GetString("filter")
		raw, _ := cmd.Flags().GetBool("raw")

		c := newClient()
		// Metrics are typically on an internal port; try the main port's /metrics first
		data, code, err := c.rawGet("/metrics")
		if err != nil {
			return fmt.Errorf("fetch metrics: %w", err)
		}
		if code >= 400 {
			return fmt.Errorf("metrics endpoint returned HTTP %d (metrics may be on internal port)", code)
		}

		if raw {
			fmt.Print(string(data))
			return nil
		}

		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "#") {
				continue
			}
			if filter != "" && !strings.Contains(line, filter) {
				continue
			}
			if filter == "" && !strings.HasPrefix(line, "arl_") {
				continue
			}
			fmt.Println(line)
		}
		return nil
	},
}

func init() {
	metricsCmd.Flags().String("filter", "", "Filter metrics by substring (e.g. 'pool', 'session')")
	metricsCmd.Flags().Bool("raw", false, "Print raw Prometheus exposition format")
}
