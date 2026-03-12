package config

import (
	"os"
	"testing"
)

func TestIsAutoNameEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.IsAutoNameEnabled() {
			t.Error("expected true when AutoNameSessions is nil")
		}
	})

	t.Run("true", func(t *testing.T) {
		v := true
		cfg := &Config{AutoNameSessions: &v}
		if !cfg.IsAutoNameEnabled() {
			t.Error("expected true")
		}
	})

	t.Run("false", func(t *testing.T) {
		v := false
		cfg := &Config{AutoNameSessions: &v}
		if cfg.IsAutoNameEnabled() {
			t.Error("expected false")
		}
	})
}

func TestIsAutoUpdateEnabled(t *testing.T) {
	t.Run("nil defaults to true", func(t *testing.T) {
		cfg := &Config{}
		if !cfg.IsAutoUpdateEnabled() {
			t.Error("expected true when AutoUpdate is nil")
		}
	})

	t.Run("true", func(t *testing.T) {
		v := true
		cfg := &Config{AutoUpdate: &v}
		if !cfg.IsAutoUpdateEnabled() {
			t.Error("expected true")
		}
	})

	t.Run("false", func(t *testing.T) {
		v := false
		cfg := &Config{AutoUpdate: &v}
		if cfg.IsAutoUpdateEnabled() {
			t.Error("expected false")
		}
	})
}

func TestGetEditor(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		cfg := &Config{Editor: "vim"}
		if got := cfg.GetEditor(); got != "vim" {
			t.Errorf("got %q, want vim", got)
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		cfg := &Config{}
		old := os.Getenv("EDITOR")
		os.Setenv("EDITOR", "nano")
		defer os.Setenv("EDITOR", old)

		if got := cfg.GetEditor(); got != "nano" {
			t.Errorf("got %q, want nano", got)
		}
	})

	t.Run("default code", func(t *testing.T) {
		cfg := &Config{}
		old := os.Getenv("EDITOR")
		os.Unsetenv("EDITOR")
		defer os.Setenv("EDITOR", old)

		if got := cfg.GetEditor(); got != "code" {
			t.Errorf("got %q, want code", got)
		}
	})
}
