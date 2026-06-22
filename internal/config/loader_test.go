package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveConfigPath(t *testing.T) {
	// Create a temp home directory to isolate tests
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	// On Windows
	t.Setenv("APPDATA", tempHome)

	// Test 1: Flag path has highest priority
	flagPath := "/some/explicit/path.yaml"
	res, err := ResolveConfigPath(flagPath)
	if err != nil || res != flagPath {
		t.Errorf("expected %s, got %s (err: %v)", flagPath, res, err)
	}

	// Test 2: Env var
	envPath := "/env/path.yaml"
	t.Setenv("LAZYBLOCKS_CONFIG", envPath)
	res, err = ResolveConfigPath("")
	if err != nil || res != envPath {
		t.Errorf("expected %s, got %s (err: %v)", envPath, res, err)
	}
	t.Setenv("LAZYBLOCKS_CONFIG", "") // clear it

	// Test 3: No config anywhere
	// Make sure we are in a temp dir so local fallback isn't found
	originalWd, _ := os.Getwd()
	os.Chdir(tempHome)
	defer os.Chdir(originalWd)

	_, err = ResolveConfigPath("")
	if err != ErrConfigNotFound {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}

	// Test 4: Default user config path
	configDir, _ := GetDefaultConfigDir()
	os.MkdirAll(configDir, 0755)
	defaultPath := filepath.Join(configDir, "config.yaml")
	os.WriteFile(defaultPath, []byte("test"), 0644)

	res, err = ResolveConfigPath("")
	if err != nil || res != defaultPath {
		t.Errorf("expected %s, got %s (err: %v)", defaultPath, res, err)
	}
}

func TestInitConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("APPDATA", tempHome)

	// First init
	path, err := InitConfig(false)
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	// Verify content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read created config: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("Config file is empty")
	}

	// Second init (should fail without force)
	_, err = InitConfig(false)
	if err == nil {
		t.Errorf("Expected error when config already exists")
	}

	// Second init with force
	_, err = InitConfig(true)
	if err != nil {
		t.Errorf("Expected success when forcing init: %v", err)
	}
}
