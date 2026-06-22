package config

import (
	"os"
	"path/filepath"
)

// GetDefaultConfigDir returns the OS-specific user configuration directory for LazyBlocks.
func GetDefaultConfigDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "lazyblocks"), nil
}

// GetDefaultConfigPath returns the absolute path to the main config file.
func GetDefaultConfigPath() (string, error) {
	dir, err := GetDefaultConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}
