package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current CLI configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		ctxName := cfg.activeContextName(flagContext)
		apiKey := effectiveAPIKey()
		cfgPath := configFilePath()
		if flagOutput == "json" {
			printJSON(map[string]string{
				"context":    ctxName,
				"gatewayURL": flagGatewayURL,
				"apiKey":     maskKey(apiKey),
				"format":     flagOutput,
				"configFile": cfgPath,
			})
			return nil
		}

		fmt.Printf("Context:      %s\n", ctxName)
		fmt.Printf("Gateway URL:  %s\n", flagGatewayURL)
		fmt.Printf("API Key:      %s\n", maskKey(apiKey))
		fmt.Printf("Format:       %s\n", flagOutput)
		fmt.Printf("Config file:  %s\n", cfgPath)
		return nil
	},
}

var configGetContextsCmd = &cobra.Command{
	Use:     "get-contexts",
	Aliases: []string{"list-contexts"},
	Short:   "List all configured contexts",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		names := cfg.contextNames()
		if len(names) == 0 {
			fmt.Println("No contexts configured.")
			return nil
		}
		if flagOutput == "json" {
			type ctxEntry struct {
				Name       string `json:"name"`
				GatewayURL string `json:"gatewayURL"`
				APIKey     string `json:"apiKey"`
				Current    bool   `json:"current"`
			}
			entries := make([]ctxEntry, 0, len(names))
			for _, name := range names {
				c := cfg.Contexts[name]
				entries = append(entries, ctxEntry{
					Name:       name,
					GatewayURL: c.GatewayURL,
					APIKey:     maskKey(c.APIKey),
					Current:    name == cfg.CurrentContext,
				})
			}
			printJSON(entries)
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintf(w, "CURRENT\tNAME\tGATEWAY URL\tAPI KEY\n")
		for _, name := range names {
			c := cfg.Contexts[name]
			marker := ""
			if name == cfg.CurrentContext {
				marker = "*"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", marker, name, c.GatewayURL, maskKey(c.APIKey))
		}
		w.Flush()
		return nil
	},
}

var configUseContextCmd = &cobra.Command{
	Use:   "use-context <name>",
	Short: "Switch the current context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		name := args[0]
		if _, ok := cfg.Contexts[name]; !ok {
			return usageError("context %q does not exist; use 'arl config set-context' to create it", name)
		}
		cfg.CurrentContext = name
		if err := saveRootConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Switched to context %q.\n", name)
		return nil
	},
}

var configCurrentContextCmd = &cobra.Command{
	Use:   "current-context",
	Short: "Show the current context name",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		name := cfg.activeContextName(flagContext)
		if flagOutput == "json" {
			printJSON(map[string]string{"currentContext": name})
			return nil
		}
		fmt.Println(name)
		return nil
	},
}

var configSetContextCmd = &cobra.Command{
	Use:   "set-context <name>",
	Short: "Create or update a context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		name := args[0]
		if cfg.Contexts == nil {
			cfg.Contexts = make(map[string]contextConfig)
		}
		ctx := cfg.Contexts[name]

		if cmd.Flags().Changed("gateway-url") {
			v, _ := cmd.Flags().GetString("gateway-url")
			ctx.GatewayURL = v
		}
		if cmd.Flags().Changed("api-key") {
			v, _ := cmd.Flags().GetString("api-key")
			ctx.APIKey = v
		}
		if cmd.Flags().Changed("api-key-file") {
			v, _ := cmd.Flags().GetString("api-key-file")
			ctx.APIKeyFile = v
		}

		cfg.Contexts[name] = ctx
		if cfg.CurrentContext == "" {
			cfg.CurrentContext = name
		}
		if err := saveRootConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Context %q set.\n", name)
		return nil
	},
}

var configDeleteContextCmd = &cobra.Command{
	Use:   "delete-context <name>",
	Short: "Delete a context",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := loadRootConfig()
		name := args[0]
		if _, ok := cfg.Contexts[name]; !ok {
			return usageError("context %q does not exist", name)
		}
		delete(cfg.Contexts, name)
		if cfg.CurrentContext == name {
			cfg.CurrentContext = ""
			for n := range cfg.Contexts {
				cfg.CurrentContext = n
				break
			}
		}
		if err := saveRootConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("Context %q deleted.\n", name)
		return nil
	},
}

func init() {
	configSetContextCmd.Flags().String("gateway-url", "", "Gateway URL for the context")
	configSetContextCmd.Flags().String("api-key", "", "API key for the context")
	configSetContextCmd.Flags().String("api-key-file", "", "API key file path for the context")

	configCmd.AddCommand(configGetContextsCmd)
	configCmd.AddCommand(configUseContextCmd)
	configCmd.AddCommand(configCurrentContextCmd)
	configCmd.AddCommand(configSetContextCmd)
	configCmd.AddCommand(configDeleteContextCmd)
}

func maskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return "****"
	}
	return key[:4] + "****" + key[len(key)-4:]
}
