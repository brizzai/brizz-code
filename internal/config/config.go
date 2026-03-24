package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/brizzai/brizz-code/internal/debuglog"
)

// Config holds user-configurable settings.
type Config struct {
	TickIntervalSec    int    `json:"tick_interval_sec,omitempty"`
	DefaultProjectPath string `json:"default_project_path,omitempty"`
	Editor             string `json:"editor,omitempty"`
	Theme              string `json:"theme,omitempty"`
	AutoNameSessions   *bool  `json:"auto_name_sessions,omitempty"`
	AutoUpdate         *bool  `json:"auto_update,omitempty"`
	CopyClaudeSettings *bool  `json:"copy_claude_settings,omitempty"`
	EnterMode          string `json:"enter_mode,omitempty"` // "attach" or "split"
	Telemetry          *bool  `json:"telemetry,omitempty"`
	SidebarPct         *int   `json:"sidebar_pct,omitempty"`
}

// IsAutoNameEnabled returns whether auto-naming is enabled (default: true).
func (c *Config) IsAutoNameEnabled() bool {
	if c.AutoNameSessions == nil {
		return true
	}
	return *c.AutoNameSessions
}

// IsAutoUpdateEnabled returns whether auto-update is enabled (default: true).
func (c *Config) IsAutoUpdateEnabled() bool {
	if c.AutoUpdate == nil {
		return true
	}
	return *c.AutoUpdate
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

	path := DefaultConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		debuglog.Logger.Info("config file not found, using defaults", "path", path)
		return cfg
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		debuglog.Logger.Error("failed to parse config file", "path", path, "error", err)
	} else {
		debuglog.Logger.Info("config loaded", "path", path)
	}

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
		debuglog.Logger.Error("failed to create config directory", "path", path, "error", err)
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		debuglog.Logger.Error("failed to marshal config", "error", err)
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		debuglog.Logger.Error("failed to write config file", "path", path, "error", err)
		return err
	}
	return nil
}

// IsCopyClaudeSettingsEnabled returns whether to copy .claude/settings.local.json to new worktrees (default: true).
func (c *Config) IsCopyClaudeSettingsEnabled() bool {
	if c.CopyClaudeSettings == nil {
		return true
	}
	return *c.CopyClaudeSettings
}

// GetEnterMode returns the configured Enter key mode ("attach" or "split").
func (c *Config) GetEnterMode() string {
	if c.EnterMode == "split" {
		return "split"
	}
	return "attach"
}

// IsTelemetryEnabled returns whether telemetry is enabled (default: true).
func (c *Config) IsTelemetryEnabled() bool {
	if c.Telemetry == nil {
		return true
	}
	return *c.Telemetry
}

// GetSidebarPct returns the sidebar width percentage, clamped to [20, 60], default 35.
func (c *Config) GetSidebarPct() int {
	if c.SidebarPct == nil {
		return 35
	}
	v := *c.SidebarPct
	if v < 20 {
		return 20
	}
	if v > 60 {
		return 60
	}
	return v
}

// SetSidebarPct sets the sidebar width percentage, clamping to [20, 60].
func (c *Config) SetSidebarPct(pct int) {
	if pct < 20 {
		pct = 20
	}
	if pct > 60 {
		pct = 60
	}
	c.SidebarPct = &pct
}

// StepSidebarPct adjusts the sidebar percentage by ~2.5 in the given direction
// (dir > 0 = grow, dir < 0 = shrink). Since the stored value is an integer, steps
// alternate between 3 and 2 to produce an effective 2.5% increment on the grid:
// 20, 23, 25, 28, 30, 33, 35, 38, 40, 43, 45, 48, 50, 53, 55, 58, 60.
func (c *Config) StepSidebarPct(dir int) {
	cur := c.GetSidebarPct()
	if dir > 0 {
		if cur%5 == 0 {
			c.SetSidebarPct(cur + 3)
		} else {
			c.SetSidebarPct(cur + 2)
		}
	} else {
		if cur%5 == 0 {
			c.SetSidebarPct(cur - 2)
		} else {
			c.SetSidebarPct(cur - 3)
		}
	}
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
