// Package migration handles one-shot upgrade from the legacy "brizz-code"
// installation layout to "fleet". It moves the config dir, renames active
// tmux sessions, strips legacy hook entries from Claude Code's settings,
// and aliases legacy env vars so in-flight Claude processes survive the
// upgrade. Safe to call on every startup; idempotent.
package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/brizzai/fleet/internal/debuglog"
	"github.com/brizzai/fleet/internal/hooks"
	"github.com/brizzai/fleet/internal/tmux"
)

const (
	legacyConfigDirName = "brizz-code"
	legacyTmuxPrefix    = "brizzcode_"
	legacyHookMarker    = "brizz-code hook-handler"
	migrationMarkerName = ".migrated-from-brizz-code"
)

// legacyEnvAliases maps new env var names to the legacy names that should
// populate them when the new name is unset.
var legacyEnvAliases = map[string]string{
	"FLEET_INSTANCE_ID":        "BRIZZCODE_INSTANCE_ID",
	"FLEET_DEBUG":              "BRIZZ_DEBUG",
	"FLEET_TELEMETRY_DISABLED": "BRIZZ_TELEMETRY_DISABLED",
	"FLEET_DEMO_PREFIX":        "BRIZZ_DEMO_PREFIX",
}

// AliasLegacyEnv copies legacy BRIZZ* env vars into FLEET_* names when the
// new ones are unset. Called early in process startup so that downstream code
// can read FLEET_* exclusively. The legacy hook-handler subprocess inherits
// env from the original brizz-code TUI; without this aliasing, the upgraded
// hook-handler binary would lose its instance ID.
func AliasLegacyEnv() {
	for newKey, legacyKey := range legacyEnvAliases {
		if os.Getenv(newKey) != "" {
			continue
		}
		if v := os.Getenv(legacyKey); v != "" {
			_ = os.Setenv(newKey, v)
		}
	}
}

// Report describes what migration did. Zero values mean nothing happened.
type Report struct {
	ConfigMigrated bool
	TmuxRenamed    int
	HooksStripped  int
}

// Run performs first-run migration. Drops a marker file inside the new
// config dir so subsequent invocations short-circuit — but only when the
// critical config-dir step did not encounter an actionable failure. tmux
// rename and hook strip are best-effort and don't gate the marker.
func Run() Report {
	var r Report
	home, err := os.UserHomeDir()
	if err != nil {
		return r
	}
	newConfigDir := filepath.Join(home, ".config", "fleet")
	legacyConfigDir := filepath.Join(home, ".config", legacyConfigDirName)
	markerPath := filepath.Join(newConfigDir, migrationMarkerName)

	if _, err := os.Stat(markerPath); err == nil {
		return r
	}

	migrated, configErr := migrateConfigDir(legacyConfigDir, newConfigDir)
	r.ConfigMigrated = migrated
	r.TmuxRenamed = renameTmuxSessions()
	r.HooksStripped = stripLegacyHooks(hooks.GetClaudeConfigDir())

	if configErr != nil {
		debuglog.Logger.Warn("migration: config-dir step failed; not writing marker so we can retry next launch",
			"err", configErr)
	} else if err := os.MkdirAll(newConfigDir, 0o755); err == nil {
		_ = os.WriteFile(markerPath, []byte("migrated from brizz-code\n"), 0o644)
	}

	if r.ConfigMigrated || r.TmuxRenamed > 0 || r.HooksStripped > 0 {
		debuglog.Logger.Info("migration: complete",
			"config_migrated", r.ConfigMigrated,
			"tmux_renamed", r.TmuxRenamed,
			"hooks_stripped", r.HooksStripped)
	}
	return r
}

// migrateConfigDir reports whether anything was moved and returns a non-nil
// error only on actionable failures the user should be able to retry (e.g.
// rename failed mid-migration). Returning (false, nil) means there was simply
// nothing to do — caller should treat that as success.
func migrateConfigDir(legacyDir, newDir string) (bool, error) {
	info, err := os.Stat(legacyDir)
	if err != nil || !info.IsDir() {
		return false, nil
	}
	// Nothing meaningful in legacy if there's no state.db.
	if !fileExists(filepath.Join(legacyDir, "state.db")) {
		return false, nil
	}

	// Fast path: new dir doesn't exist — atomic rename.
	if _, err := os.Stat(newDir); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(newDir), 0o755); err != nil {
			return false, fmt.Errorf("create config parent: %w", err)
		}
		if err := os.Rename(legacyDir, newDir); err != nil {
			return false, fmt.Errorf("rename %s -> %s: %w", legacyDir, newDir, err)
		}
		return true, nil
	}

	// New dir exists but has no state.db — usually because chrome-host or another
	// subprocess called debuglog.Init() first and created an empty dir + debug.log.
	// Merge legacy contents in without clobbering anything that's already there.
	if !fileExists(filepath.Join(newDir, "state.db")) {
		return mergeLegacyDir(legacyDir, newDir)
	}

	// Both have state.db — user likely created the new dir manually or ran fleet
	// before this migration shipped. Don't clobber their data and don't error,
	// but do log so they notice. The marker still gets written so we don't
	// re-warn forever.
	debuglog.Logger.Warn("migration: both legacy and new config dirs have state.db; skipping move",
		"legacy", legacyDir, "new", newDir)
	return false, nil
}

// mergeLegacyDir moves entries from legacyDir into newDir, skipping any that
// already exist in newDir. Returns whether anything moved and an error if the
// legacy dir couldn't be read at all. Per-file move failures are logged but do
// not fail the overall step — partial progress is preferable to no progress.
func mergeLegacyDir(legacyDir, newDir string) (bool, error) {
	entries, err := os.ReadDir(legacyDir)
	if err != nil {
		return false, fmt.Errorf("read legacy dir %s: %w", legacyDir, err)
	}
	moved := 0
	for _, e := range entries {
		legacyPath := filepath.Join(legacyDir, e.Name())
		newPath := filepath.Join(newDir, e.Name())
		if _, err := os.Stat(newPath); err == nil {
			continue
		}
		if err := os.Rename(legacyPath, newPath); err != nil {
			debuglog.Logger.Warn("migration: failed to move file",
				"from", legacyPath, "to", newPath, "err", err)
			continue
		}
		moved++
	}
	if moved > 0 {
		// Try to remove the (possibly-empty) legacy dir; ignore errors — sockets
		// or files we couldn't move keep it alive.
		_ = os.Remove(legacyDir)
	}
	return moved > 0, nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// tmuxRunner is the seam tests stub to avoid touching the live tmux server.
// Production code shells out to the real tmux binary.
var tmuxRunner tmuxExec = realTmuxExec{}

type tmuxExec interface {
	List() ([]string, error)
	Rename(old, newName string) error
}

type realTmuxExec struct{}

func (realTmuxExec) List() ([]string, error) {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
}

func (realTmuxExec) Rename(old, newName string) error {
	return exec.Command("tmux", "rename-session", "-t", old, newName).Run()
}

func renameTmuxSessions() int {
	names, err := tmuxRunner.List()
	if err != nil {
		return 0
	}
	var renamed int
	for _, name := range names {
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, legacyTmuxPrefix) {
			continue
		}
		newName := tmux.SessionPrefix + strings.TrimPrefix(name, legacyTmuxPrefix)
		if err := tmuxRunner.Rename(name, newName); err != nil {
			debuglog.Logger.Warn("migration: tmux rename failed", "old", name, "new", newName, "err", err)
			continue
		}
		renamed++
	}
	return renamed
}

func stripLegacyHooks(claudeConfigDir string) int {
	settingsPath := filepath.Join(claudeConfigDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return 0
	}

	var rawSettings map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawSettings); err != nil {
		return 0
	}
	rawHooks, ok := rawSettings["hooks"]
	if !ok {
		return 0
	}
	var events map[string]json.RawMessage
	if err := json.Unmarshal(rawHooks, &events); err != nil {
		return 0
	}

	removed := 0
	for evtName, evtRaw := range events {
		cleaned, n := stripLegacyFromEvent(evtRaw)
		removed += n
		if cleaned == nil {
			delete(events, evtName)
		} else {
			events[evtName] = cleaned
		}
	}
	if removed == 0 {
		return 0
	}

	if len(events) == 0 {
		delete(rawSettings, "hooks")
	} else {
		b, err := json.MarshalIndent(events, "", "  ")
		if err != nil {
			return removed
		}
		rawSettings["hooks"] = b
	}
	out, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return removed
	}
	if err := os.WriteFile(settingsPath, out, 0o644); err != nil {
		debuglog.Logger.Warn("migration: failed to write cleaned settings.json", "err", err)
	}
	return removed
}

func stripLegacyFromEvent(raw json.RawMessage) (json.RawMessage, int) {
	var matchers []map[string]any
	if err := json.Unmarshal(raw, &matchers); err != nil {
		return raw, 0
	}
	removed := 0
	keptMatchers := matchers[:0]
	for _, m := range matchers {
		hooksRaw, _ := m["hooks"].([]any)
		var keptHooks []any
		for _, h := range hooksRaw {
			hookMap, _ := h.(map[string]any)
			cmd, _ := hookMap["command"].(string)
			if strings.Contains(cmd, legacyHookMarker) {
				removed++
				continue
			}
			keptHooks = append(keptHooks, h)
		}
		if len(keptHooks) == 0 {
			continue
		}
		m["hooks"] = keptHooks
		keptMatchers = append(keptMatchers, m)
	}
	if len(keptMatchers) == 0 {
		return nil, removed
	}
	b, err := json.Marshal(keptMatchers)
	if err != nil {
		return raw, removed
	}
	return b, removed
}
