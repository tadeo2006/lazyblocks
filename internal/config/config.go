package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Instances []Instance `yaml:"instances"`
}

type Instance struct {
	ID            string `yaml:"id"`
	Name          string `yaml:"name"`
	Type          string `yaml:"type"`
	MCVersion     string `yaml:"mc_version,omitempty"`
	ContainerName string `yaml:"container_name"`
	Memory        string `yaml:"memory,omitempty"`
	RCON          RCON   `yaml:"rcon"`
	Paths         Paths  `yaml:"paths"`
	Backup        Backup `yaml:"backup"`
}

type RCON struct {
	Enabled     bool   `yaml:"enabled"`
	Host        string `yaml:"host"`
	Port        int    `yaml:"port"`
	PasswordEnv string `yaml:"password_env"`
}

type Paths struct {
	DataDir string `yaml:"data_dir"`
}

type Backup struct {
	Keep     int `yaml:"keep"`
	Interval int `yaml:"interval"`
}


// SaveConfig writes the structured configuration back to the YAML file.
func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("error serializing yaml: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}
