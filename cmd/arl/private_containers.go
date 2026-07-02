package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

const (
	privateContainerFlag      = "private-container"
	privateContainersFileFlag = "private-containers-file"
)

func addPrivateContainerFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray(privateContainerFlag, nil, "Private container JSON object; repeatable")
	cmd.Flags().String(privateContainersFileFlag, "", "Path to a JSON object or array of private container specs")
}

func privateContainersFromFlags(cmd *cobra.Command) ([]PrivateContainerSpec, error) {
	inlineSpecs, _ := cmd.Flags().GetStringArray(privateContainerFlag)
	filePath, _ := cmd.Flags().GetString(privateContainersFileFlag)
	var containers []PrivateContainerSpec

	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("read --%s: %w", privateContainersFileFlag, err)
		}
		fileContainers, err := decodePrivateContainers(data)
		if err != nil {
			return nil, fmt.Errorf("decode --%s: %w", privateContainersFileFlag, err)
		}
		containers = append(containers, fileContainers...)
	}

	for _, raw := range inlineSpecs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, usageError("--%s must not be empty", privateContainerFlag)
		}
		inlineContainers, err := decodePrivateContainers([]byte(raw))
		if err != nil {
			return nil, fmt.Errorf("decode --%s: %w", privateContainerFlag, err)
		}
		containers = append(containers, inlineContainers...)
	}

	if len(containers) == 0 {
		return nil, nil
	}
	return containers, nil
}

func decodePrivateContainers(data []byte) ([]PrivateContainerSpec, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, usageError("private container JSON must not be empty")
	}
	if bytes.HasPrefix(data, []byte("[")) {
		var containers []PrivateContainerSpec
		if err := json.Unmarshal(data, &containers); err != nil {
			return nil, err
		}
		return containers, nil
	}

	var container PrivateContainerSpec
	if err := json.Unmarshal(data, &container); err != nil {
		return nil, err
	}
	return []PrivateContainerSpec{container}, nil
}

func envMapFromFlags(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(values))
	for _, value := range values {
		key, val, ok := strings.Cut(value, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, usageError("--env must be in KEY=VALUE form")
		}
		env[key] = val
	}
	return env, nil
}
