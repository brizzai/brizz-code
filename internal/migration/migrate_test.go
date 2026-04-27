package migration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAliasLegacyEnv(t *testing.T) {
	t.Run("aliases legacy when new is unset", func(t *testing.T) {
		t.Setenv("FLEET_INSTANCE_ID", "")
		t.Setenv("BRIZZCODE_INSTANCE_ID", "abc123")
		AliasLegacyEnv()
		if got := os.Getenv("FLEET_INSTANCE_ID"); got != "abc123" {
			t.Errorf("FLEET_INSTANCE_ID: got %q, want %q", got, "abc123")
		}
	})

	t.Run("preserves new when both are set", func(t *testing.T) {
		t.Setenv("FLEET_DEBUG", "new")
		t.Setenv("BRIZZ_DEBUG", "old")
		AliasLegacyEnv()
		if got := os.Getenv("FLEET_DEBUG"); got != "new" {
			t.Errorf("FLEET_DEBUG: got %q, want %q", got, "new")
		}
	})

	t.Run("no-op when neither is set", func(t *testing.T) {
		t.Setenv("FLEET_TELEMETRY_DISABLED", "")
		t.Setenv("BRIZZ_TELEMETRY_DISABLED", "")
		AliasLegacyEnv()
		if got := os.Getenv("FLEET_TELEMETRY_DISABLED"); got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})
}

func TestMigrateConfigDir(t *testing.T) {
	t.Run("moves legacy dir when new does not exist", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		newDir := filepath.Join(base, "fleet")
		if err := os.MkdirAll(filepath.Join(legacy, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("dbdata"), 0o644); err != nil {
			t.Fatal(err)
		}

		moved, err := migrateConfigDir(legacy, newDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !moved {
			t.Fatal("expected migrateConfigDir to return moved=true")
		}
		if _, err := os.Stat(legacy); !os.IsNotExist(err) {
			t.Error("legacy dir should be gone after migration")
		}
		if _, err := os.Stat(filepath.Join(newDir, "state.db")); err != nil {
			t.Errorf("state.db not present in new dir: %v", err)
		}
		if _, err := os.Stat(filepath.Join(newDir, "hooks")); err != nil {
			t.Errorf("hooks dir not present in new dir: %v", err)
		}
	})

	t.Run("no-op when legacy does not exist", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		newDir := filepath.Join(base, "fleet")
		moved, err := migrateConfigDir(legacy, newDir)
		if err != nil {
			t.Errorf("expected nil err when legacy absent, got %v", err)
		}
		if moved {
			t.Error("expected moved=false when legacy is absent")
		}
	})

	t.Run("no-op when legacy has no state.db", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		newDir := filepath.Join(base, "fleet")
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "debug.log"), []byte("logs"), 0o644); err != nil {
			t.Fatal(err)
		}
		moved, err := migrateConfigDir(legacy, newDir)
		if err != nil {
			t.Errorf("expected nil err when legacy lacks state.db, got %v", err)
		}
		if moved {
			t.Error("expected moved=false when legacy lacks state.db")
		}
	})

	t.Run("merges into stub new dir created by chrome-host pre-init", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		newDir := filepath.Join(base, "fleet")

		// Legacy has the real data.
		if err := os.MkdirAll(filepath.Join(legacy, "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("realdata"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "config.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}

		// New dir is a stub: only debug.log (chrome-host pre-init scenario), no state.db.
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(newDir, "debug.log"), []byte("stub"), 0o644); err != nil {
			t.Fatal(err)
		}

		moved, err := migrateConfigDir(legacy, newDir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !moved {
			t.Fatal("expected migrateConfigDir to return moved=true (stub merge)")
		}
		// state.db moved across.
		got, err := os.ReadFile(filepath.Join(newDir, "state.db"))
		if err != nil {
			t.Fatalf("state.db missing: %v", err)
		}
		if string(got) != "realdata" {
			t.Errorf("state.db contents: got %q, want %q", got, "realdata")
		}
		// hooks dir moved across.
		if _, err := os.Stat(filepath.Join(newDir, "hooks")); err != nil {
			t.Errorf("hooks dir missing: %v", err)
		}
		// debug.log preserved (the stub one wins because it was already there).
		got, _ = os.ReadFile(filepath.Join(newDir, "debug.log"))
		if string(got) != "stub" {
			t.Errorf("debug.log got clobbered: %q", got)
		}
	})

	t.Run("skips when both have state.db", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		newDir := filepath.Join(base, "fleet")
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(newDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(newDir, "state.db"), []byte("new"), 0o644); err != nil {
			t.Fatal(err)
		}
		moved, err := migrateConfigDir(legacy, newDir)
		if err != nil {
			t.Errorf("expected nil err when both have state.db, got %v", err)
		}
		if moved {
			t.Error("expected moved=false when both have state.db")
		}
		got, _ := os.ReadFile(filepath.Join(newDir, "state.db"))
		if string(got) != "new" {
			t.Errorf("new state.db should not be clobbered, got %q", got)
		}
	})

	t.Run("returns error when rename target's parent is unwritable", func(t *testing.T) {
		base := t.TempDir()
		legacy := filepath.Join(base, "brizz-code")
		// Put the new dir under a path whose parent we make read-only so MkdirAll fails.
		readOnlyParent := filepath.Join(base, "readonly")
		if err := os.MkdirAll(readOnlyParent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(legacy, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("dbdata"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(readOnlyParent, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(readOnlyParent, 0o755) })

		newDir := filepath.Join(readOnlyParent, "deeper", "fleet")
		moved, err := migrateConfigDir(legacy, newDir)
		if err == nil {
			t.Fatal("expected error when MkdirAll on parent fails")
		}
		if moved {
			t.Error("expected moved=false on failure")
		}
		if _, statErr := os.Stat(filepath.Join(legacy, "state.db")); statErr != nil {
			t.Errorf("legacy state.db should still be intact after failed migration: %v", statErr)
		}
	})
}

func TestRunDoesNotWriteMarkerOnFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(tmp, "claude"))

	prev := tmuxRunner
	tmuxRunner = &fakeTmux{sessions: []string{}}
	t.Cleanup(func() { tmuxRunner = prev })

	// Force migrateConfigDir to fail by making ~/.config a read-only file
	// (so MkdirAll for the new dir's parent fails). Setting up a real-world
	// failure mode without mocking the os package.
	configParent := filepath.Join(tmp, ".config")
	if err := os.WriteFile(configParent, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// Even with config wedged, Run should swallow the failure and not write the marker.
	r := Run()
	if r.ConfigMigrated {
		t.Error("expected ConfigMigrated=false on failure")
	}

	// Marker must not exist — that's the whole point of this test.
	markerPath := filepath.Join(tmp, ".config", "fleet", ".migrated-from-brizz-code")
	if _, err := os.Stat(markerPath); err == nil {
		t.Error("marker file should not be written when config migration is blocked")
	}
}

func TestStripLegacyHooks(t *testing.T) {
	t.Run("removes legacy entries and preserves others", func(t *testing.T) {
		dir := t.TempDir()
		settingsPath := filepath.Join(dir, "settings.json")
		settings := map[string]any{
			"hooks": map[string]any{
				"Stop": []map[string]any{
					{
						"hooks": []map[string]any{
							{"type": "command", "command": "/usr/local/bin/brizz-code hook-handler", "async": true},
							{"type": "command", "command": "other-tool"},
						},
					},
				},
				"UserPromptSubmit": []map[string]any{
					{
						"hooks": []map[string]any{
							{"type": "command", "command": "brizz-code hook-handler", "async": true},
						},
					},
				},
			},
			"theme": "dark",
		}
		data, _ := json.Marshal(settings)
		if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
			t.Fatal(err)
		}

		removed := stripLegacyHooks(dir)
		if removed != 2 {
			t.Errorf("removed: got %d, want 2", removed)
		}

		raw, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		got := string(raw)
		if strings.Contains(got, "brizz-code hook-handler") {
			t.Errorf("legacy marker still present: %s", got)
		}
		if !strings.Contains(got, "other-tool") {
			t.Errorf("non-legacy hook stripped: %s", got)
		}
		if !strings.Contains(got, `"theme"`) {
			t.Errorf("non-hook setting was lost: %s", got)
		}
	})

	t.Run("no settings.json is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		if got := stripLegacyHooks(dir); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})

	t.Run("settings without hooks key is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := stripLegacyHooks(dir); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
		raw, _ := os.ReadFile(path)
		if !strings.Contains(string(raw), `"theme"`) {
			t.Errorf("settings should be untouched")
		}
	})
}

// fakeTmux records calls and never touches the real tmux server.
type fakeTmux struct {
	sessions []string
	renames  map[string]string
	listErr  error
}

func (f *fakeTmux) List() ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.sessions, nil
}

func (f *fakeTmux) Rename(old, newName string) error {
	if f.renames == nil {
		f.renames = map[string]string{}
	}
	f.renames[old] = newName
	return nil
}

func TestRenameTmuxSessions(t *testing.T) {
	t.Run("renames only legacy prefix", func(t *testing.T) {
		fake := &fakeTmux{sessions: []string{
			"brizzcode_alpha_aaaa1111",
			"fleet_beta_bbbb2222",
			"unrelated_session",
			"brizzcode_gamma_cccc3333",
		}}
		prev := tmuxRunner
		tmuxRunner = fake
		t.Cleanup(func() { tmuxRunner = prev })

		got := renameTmuxSessions()
		if got != 2 {
			t.Errorf("renamed: got %d, want 2", got)
		}
		want := map[string]string{
			"brizzcode_alpha_aaaa1111": "fleet_alpha_aaaa1111",
			"brizzcode_gamma_cccc3333": "fleet_gamma_cccc3333",
		}
		for old, expected := range want {
			if fake.renames[old] != expected {
				t.Errorf("rename %s: got %q, want %q", old, fake.renames[old], expected)
			}
		}
		if _, ok := fake.renames["fleet_beta_bbbb2222"]; ok {
			t.Error("should not rename sessions already on new prefix")
		}
	})
}

func TestRunIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Point Claude config dir at a temp location so we don't touch real ~/.claude.
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(tmp, "claude"))

	// Stub tmux so the test never touches the real tmux server.
	prev := tmuxRunner
	tmuxRunner = &fakeTmux{sessions: []string{}}
	t.Cleanup(func() { tmuxRunner = prev })

	legacy := filepath.Join(tmp, ".config", "brizz-code")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "state.db"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	r1 := Run()
	if !r1.ConfigMigrated {
		t.Error("first run: expected ConfigMigrated=true")
	}

	r2 := Run()
	if r2.ConfigMigrated {
		t.Error("second run: expected ConfigMigrated=false (marker should short-circuit)")
	}

	newDir := filepath.Join(tmp, ".config", "fleet")
	if _, err := os.Stat(filepath.Join(newDir, ".migrated-from-brizz-code")); err != nil {
		t.Errorf("marker file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(newDir, "state.db")); err != nil {
		t.Errorf("state.db missing in new dir: %v", err)
	}
}
