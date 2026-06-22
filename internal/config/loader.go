package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var ErrConfigNotFound = errors.New("configuration not found")

// ResolveConfigPath determines the active configuration file path based on priority:
// 1. Explicit path (flag)
// 2. LAZYBLOCKS_CONFIG environment variable
// 3. User config directory (e.g. ~/.config/lazyblocks/config.yaml)
// 4. Local fallback (./configs/local.yaml) for development
func ResolveConfigPath(flagPath string) (string, error) {
	// 1. Explicit flag
	if flagPath != "" {
		return flagPath, nil
	}

	// 2. Env Var
	if envPath := os.Getenv("LAZYBLOCKS_CONFIG"); envPath != "" {
		return envPath, nil
	}

	// 3. User config dir
	defaultPath, err := GetDefaultConfigPath()
	if err == nil {
		if _, err := os.Stat(defaultPath); err == nil {
			return defaultPath, nil
		}
	}

	// 4. Local dev fallback
	localFallback := "configs/local.yaml"
	if _, err := os.Stat(localFallback); err == nil {
		return localFallback, nil
	}

	return "", ErrConfigNotFound
}

// Load reads and parses the configuration from the resolved path.
func Load(flagPath string) (*Config, string, error) {
	resolvedPath, err := ResolveConfigPath(flagPath)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if os.IsPermission(err) {
			return nil, resolvedPath, fmt.Errorf("insufficient permissions to read config file at %s", resolvedPath)
		}
		return nil, resolvedPath, fmt.Errorf("error reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, resolvedPath, fmt.Errorf("error parsing yaml: %w", err)
	}

	if err := Validate(&cfg); err != nil {
		return nil, resolvedPath, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, resolvedPath, nil
}

// InitConfig creates the default configuration directory and file if it doesn't exist.
// If force is true, it overwrites any existing file.
func InitConfig(force bool) (string, error) {
	configDir, err := GetDefaultConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")

	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, fmt.Errorf("configuration file already exists at %s", configPath)
		}
	}

	if err := os.WriteFile(configPath, DefaultConfig, 0644); err != nil {
		return "", fmt.Errorf("failed to write default config: %w", err)
	}

	return configPath, nil
}
