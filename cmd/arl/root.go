package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	flagGatewayURL string
	flagAPIKey     string
	flagAPIKeyFile string
	flagFormat     string
	flagOutput     string
	flagNoColor    bool
	flagDumpSchema bool
	resolvedAPIKey string
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:               "arl",
	Short:             "ARL — CLI for Agentic RL runtime",
	Long:              "Command-line interface for managing ARL experiments, pools, sessions, and diagnostics.",
	Version:           version,
	SilenceUsage:      true,
	SilenceErrors:     true,
	PersistentPreRunE: configureRuntime,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagDumpSchema {
			printJSON(commandSchemaFor(cmd.Root()))
			return nil
		}
		return cmd.Help()
	},
}

func init() {
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &cliError{code: exitUsage, err: err}
	})

	defaultFormat := envOrDefault("ARL_FORMAT", envOrDefault("ARL_OUTPUT_FORMAT", "table"))
	rootCmd.PersistentFlags().StringVarP(&flagGatewayURL, "gateway-url", "g", envOrDefault("ARL_GATEWAY_URL", "http://localhost:8080"), "Gateway API base URL")
	rootCmd.PersistentFlags().StringVarP(&flagAPIKey, "api-key", "k", "", "API key for authentication (prefer ARL_API_KEY or --api-key-file)")
	rootCmd.PersistentFlags().StringVar(&flagAPIKeyFile, "api-key-file", "", "Read API key from file")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", defaultFormat, "Output format: table, json, wide")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", defaultFormat, "Legacy alias for --format: table, json, wide")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "Disable ANSI color in human-readable output")
	rootCmd.Flags().BoolVar(&flagDumpSchema, "dump-schema", false, "Print command schema as JSON and exit")

	rootCmd.AddCommand(expCmd)
	rootCmd.AddCommand(poolCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(metricsCmd)
	rootCmd.AddCommand(configCmd)
}

func configureRuntime(cmd *cobra.Command, args []string) error {
	if formatChanged(cmd, "format") && formatChanged(cmd, "output") && flagFormat != flagOutput {
		return usageError("--format and --output disagree; use only --format")
	}
	if formatChanged(cmd, "format") {
		flagOutput = flagFormat
	} else {
		flagFormat = flagOutput
	}
	if err := validateFormat(flagOutput); err != nil {
		return err
	}

	if os.Getenv("NO_COLOR") != "" {
		flagNoColor = true
	}

	apiKey, err := resolveAPIKey()
	if err != nil {
		return err
	}
	resolvedAPIKey = apiKey
	return nil
}

func formatChanged(cmd *cobra.Command, name string) bool {
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return flag.Changed
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return flag.Changed
	}
	return false
}

func validateFormat(format string) error {
	switch format {
	case "table", "json", "wide":
		return nil
	default:
		return usageError("invalid --format %q; expected one of: table, json, wide", format)
	}
}

func resolveAPIKey() (string, error) {
	if flagAPIKey != "" && flagAPIKeyFile != "" {
		return "", usageError("--api-key and --api-key-file are mutually exclusive")
	}
	if flagAPIKey != "" {
		return flagAPIKey, nil
	}
	if flagAPIKeyFile != "" {
		data, err := os.ReadFile(flagAPIKeyFile)
		if err != nil {
			return "", &cliError{code: exitEnvironment, err: fmt.Errorf("read --api-key-file: %w", err)}
		}
		key := strings.TrimSpace(string(data))
		if key == "" {
			return "", usageError("--api-key-file %q is empty", flagAPIKeyFile)
		}
		return key, nil
	}
	return os.Getenv("ARL_API_KEY"), nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func newClient() *Client {
	return NewClient(flagGatewayURL, effectiveAPIKey())
}

func effectiveAPIKey() string {
	return resolvedAPIKey
}
