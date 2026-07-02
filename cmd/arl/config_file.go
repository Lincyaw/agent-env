package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type contextConfig struct {
	GatewayURL string `yaml:"gateway_url,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	APIKeyFile string `yaml:"api_key_file,omitempty"`
}

type rootConfig struct {
	CurrentContext string                   `yaml:"current_context,omitempty"`
	Contexts       map[string]contextConfig `yaml:"contexts,omitempty"`
	Format         string                   `yaml:"format,omitempty"`
	NoColor        bool                     `yaml:"no_color,omitempty"`

	// Legacy flat fields — migrated on first load.
	GatewayURL string `yaml:"gateway_url,omitempty"`
	APIKey     string `yaml:"api_key,omitempty"`
	APIKeyFile string `yaml:"api_key_file,omitempty"`
}

func configDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "arl")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "arl")
}

func configFilePath() string {
	dir := configDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "config.yaml")
}

func loadRootConfig() rootConfig {
	path := configFilePath()
	if path == "" {
		return rootConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return rootConfig{}
	}
	var cfg rootConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return rootConfig{}
	}
	cfg.migrateLegacy()
	return cfg
}

// migrateLegacy moves flat gateway_url/api_key into a "default" context.
func (c *rootConfig) migrateLegacy() {
	gw := strings.TrimSpace(c.GatewayURL)
	key := strings.TrimSpace(c.APIKey)
	keyFile := strings.TrimSpace(c.APIKeyFile)
	if gw == "" && key == "" && keyFile == "" {
		return
	}
	if c.Contexts == nil {
		c.Contexts = make(map[string]contextConfig)
	}
	if _, exists := c.Contexts["default"]; !exists {
		c.Contexts["default"] = contextConfig{
			GatewayURL: gw,
			APIKey:     key,
			APIKeyFile: keyFile,
		}
	}
	if c.CurrentContext == "" {
		c.CurrentContext = "default"
	}
	c.GatewayURL = ""
	c.APIKey = ""
	c.APIKeyFile = ""
}

func (c *rootConfig) activeContextName(flagOverride string) string {
	if flagOverride != "" {
		return flagOverride
	}
	if c.CurrentContext != "" {
		return c.CurrentContext
	}
	return "default"
}

func (c *rootConfig) activeContext(flagOverride string) contextConfig {
	name := c.activeContextName(flagOverride)
	if c.Contexts == nil {
		return contextConfig{}
	}
	return c.Contexts[name]
}

func (c *rootConfig) contextNames() []string {
	names := make([]string, 0, len(c.Contexts))
	for name := range c.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func saveRootConfig(cfg rootConfig) error {
	path := configFilePath()
	if path == "" {
		return fmt.Errorf("cannot determine config file path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
