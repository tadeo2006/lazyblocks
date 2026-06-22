package config

import "fmt"

// Validate checks for semantic correctness and required fields in the configuration.
func Validate(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	for i, inst := range cfg.Instances {
		if inst.ID == "" {
			return fmt.Errorf("instance at index %d is missing 'id'", i)
		}
		if inst.Name == "" {
			return fmt.Errorf("instance '%s' is missing 'name'", inst.ID)
		}
		if inst.ContainerName == "" {
			return fmt.Errorf("instance '%s' is missing 'container_name'", inst.ID)
		}
		if inst.Paths.DataDir == "" {
			return fmt.Errorf("instance '%s' is missing 'paths.data_dir'", inst.ID)
		}
	}

	return nil
}
