package main

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagGatewayURL string
	flagAPIKey     string
	flagNamespace  string
	flagOutput     string
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:          "arl",
	Short:        "ARL — CLI for Agentic RL runtime",
	Long:         "Command-line interface for managing ARL experiments, pools, sessions, and diagnostics.",
	Version:      version,
	SilenceUsage: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&flagGatewayURL, "gateway-url", "g", envOrDefault("ARL_GATEWAY_URL", "http://localhost:8080"), "Gateway API base URL")
	rootCmd.PersistentFlags().StringVarP(&flagAPIKey, "api-key", "k", os.Getenv("ARL_API_KEY"), "API key for authentication")
	rootCmd.PersistentFlags().StringVarP(&flagNamespace, "namespace", "n", envOrDefault("ARL_NAMESPACE", "default"), "Kubernetes namespace")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", "table", "Output format: table, json, wide")

	rootCmd.AddCommand(expCmd)
	rootCmd.AddCommand(poolCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(metricsCmd)
	rootCmd.AddCommand(configCmd)
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newClient() *Client {
	return NewClient(flagGatewayURL, flagAPIKey)
}
