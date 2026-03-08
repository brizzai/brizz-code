package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user-configurable settings.
type Config struct {
	TickIntervalSec    int    `json:"tick_interval_sec,omitempty"`
	DefaultProjectPath string `json:"default_project_path,omitempty"`
	Editor             string `json:"editor,omitempty"`
}

// DefaultConfigPath returns the default config file path.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "brizz-code", "config.json")
}

// Load reads config from disk, returning defaults if missing.
func Load() *Config {
	cfg := &Config{
		TickIntervalSec: 2,
	}

	data, err := os.ReadFile(DefaultConfigPath())
	if err != nil {
		return cfg
	}

	_ = json.Unmarshal(data, cfg)

	// Enforce minimums.
	if cfg.TickIntervalSec < 1 {
		cfg.TickIntervalSec = 2
	}

	return cfg
}

// Save writes config to disk.
func (c *Config) Save() error {
	path := DefaultConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// GetEditor returns the configured editor, falling back to $EDITOR then "code".
func (c *Config) GetEditor() string {
	if c.Editor != "" {
		return c.Editor
	}
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	return "code"
}
