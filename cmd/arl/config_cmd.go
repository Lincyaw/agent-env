package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current CLI configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagOutput == "json" {
			printJSON(map[string]string{
				"gatewayURL": flagGatewayURL,
				"namespace":  flagNamespace,
				"apiKey":     maskKey(flagAPIKey),
				"output":     flagOutput,
			})
			return nil
		}

		fmt.Printf("Gateway URL:  %s\n", flagGatewayURL)
		fmt.Printf("Namespace:    %s\n", flagNamespace)
		fmt.Printf("API Key:      %s\n", maskKey(flagAPIKey))
		fmt.Printf("Output:       %s\n", flagOutput)
		return nil
	},
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
