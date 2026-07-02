package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var (
	flagContext    string
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

	cfg := loadRootConfig()
	ctx := cfg.activeContext(os.Getenv("ARL_CONTEXT"))

	defaultGatewayURL := fileOrDefault(ctx.GatewayURL, "http://localhost:8080")
	defaultGatewayURL = envOrDefault("ARL_GATEWAY_URL", defaultGatewayURL)

	defaultFormat := fileOrDefault(cfg.Format, "table")
	defaultFormat = envOrDefault("ARL_FORMAT", envOrDefault("ARL_OUTPUT_FORMAT", defaultFormat))

	defaultAPIKeyFile := ctx.APIKeyFile

	rootCmd.PersistentFlags().StringVar(&flagContext, "context", "", "Config context to use (overrides current_context)")
	rootCmd.PersistentFlags().StringVarP(&flagGatewayURL, "gateway-url", "g", defaultGatewayURL, "Gateway API base URL")
	rootCmd.PersistentFlags().StringVarP(&flagAPIKey, "api-key", "k", "", "API key for authentication (prefer config file or ARL_API_KEY)")
	rootCmd.PersistentFlags().StringVar(&flagAPIKeyFile, "api-key-file", defaultAPIKeyFile, "Read API key from file")
	rootCmd.PersistentFlags().StringVar(&flagFormat, "format", defaultFormat, "Output format: table, json, wide")
	rootCmd.PersistentFlags().StringVarP(&flagOutput, "output", "o", defaultFormat, "Legacy alias for --format: table, json, wide")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", cfg.NoColor, "Disable ANSI color in human-readable output")
	rootCmd.Flags().BoolVar(&flagDumpSchema, "dump-schema", false, "Print command schema as JSON and exit")

	rootCmd.AddCommand(expCmd)
	rootCmd.AddCommand(poolCmd)
	rootCmd.AddCommand(sessionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(metricsCmd)
	rootCmd.AddCommand(configCmd)
}

func fileOrDefault(fileVal, def string) string {
	if fileVal != "" {
		return fileVal
	}
	return def
}

func configureRuntime(cmd *cobra.Command, args []string) error {
	// If --context was passed as a flag, re-resolve from that context.
	// Explicit --context overrides env vars — only an explicit --gateway-url flag wins.
	if flagContext != "" {
		cfg := loadRootConfig()
		ctx := cfg.activeContext(flagContext)
		if !cmd.Flags().Changed("gateway-url") && ctx.GatewayURL != "" {
			flagGatewayURL = ctx.GatewayURL
		}
		if !cmd.Flags().Changed("api-key-file") && ctx.APIKeyFile != "" {
			flagAPIKeyFile = ctx.APIKeyFile
		}
	}

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
	cfg := loadRootConfig()
	ctxName := flagContext
	if ctxName == "" {
		ctxName = os.Getenv("ARL_CONTEXT")
	}
	// Explicit --context overrides ARL_API_KEY env var.
	if flagContext != "" {
		ctx := cfg.activeContext(ctxName)
		if key := strings.TrimSpace(ctx.APIKey); key != "" {
			return key, nil
		}
	}
	if envKey := os.Getenv("ARL_API_KEY"); envKey != "" {
		return envKey, nil
	}
	ctx := cfg.activeContext(ctxName)
	return strings.TrimSpace(ctx.APIKey), nil
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
