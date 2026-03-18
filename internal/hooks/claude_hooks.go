package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuvalhayke/brizz-code/internal/debuglog"
)

// brizzCodeHookMarker is the substring used to identify brizz-code hooks in settings.json.
const brizzCodeHookMarker = "brizz-code hook-handler"

// claudeHookEntry represents a single hook entry in Claude Code settings.
type claudeHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Async   bool   `json:"async,omitempty"`
}

// claudeHookMatcher represents a matcher block in settings.
type claudeHookMatcher struct {
	Matcher string            `json:"matcher,omitempty"`
	Hooks   []claudeHookEntry `json:"hooks"`
}

// hookEventConfig defines which Claude Code events we subscribe to.
var hookEventConfigs = []struct {
	Event   string
	Matcher string
	Async   bool
}{
	{Event: "UserPromptSubmit", Async: true},
	{Event: "Stop", Async: true},
	{Event: "PermissionRequest", Async: true},
	{Event: "Notification", Matcher: "permission_prompt|elicitation_dialog", Async: true},
	{Event: "Notification", Matcher: "idle_prompt", Async: true},
	{Event: "SessionStart", Async: true},
	{Event: "SessionEnd", Async: true},
}

// GetClaudeConfigDir returns the Claude Code config directory.
func GetClaudeConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".claude")
	}
	return filepath.Join(home, ".claude")
}

// GetHookCommand returns the full hook command string using the current binary path.
func GetHookCommand() string {
	exe, err := os.Executable()
	if err != nil {
		return "brizz-code hook-handler"
	}
	// Resolve symlinks for stable path.
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return exe + " hook-handler"
	}
	return resolved + " hook-handler"
}

// brizzCodeHook returns a hook entry with the current binary path.
func brizzCodeHook(async bool) claudeHookEntry {
	return claudeHookEntry{
		Type:    "command",
		Command: GetHookCommand(),
		Async:   async,
	}
}

// InjectClaudeHooks injects brizz-code hook entries into Claude Code's settings.json.
// Returns true if hooks were newly installed, false if already present.
func InjectClaudeHooks(configDir string) (bool, error) {
	settingsPath := filepath.Join(configDir, "settings.json")

	var rawSettings map[string]json.RawMessage
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if !os.IsNotExist(err) {
			debuglog.Logger.Error("claude hooks: failed to read settings.json", "path", settingsPath, "err", err)
			return false, fmt.Errorf("read settings.json: %w", err)
		}
		rawSettings = make(map[string]json.RawMessage)
	} else {
		if err := json.Unmarshal(data, &rawSettings); err != nil {
			debuglog.Logger.Error("claude hooks: failed to parse settings.json", "path", settingsPath, "err", err)
			return false, fmt.Errorf("parse settings.json: %w", err)
		}
	}

	var existingHooks map[string]json.RawMessage
	if raw, ok := rawSettings["hooks"]; ok {
		if err := json.Unmarshal(raw, &existingHooks); err != nil {
			debuglog.Logger.Error("claude hooks: failed to parse hooks section", "err", err)
			existingHooks = make(map[string]json.RawMessage)
		}
	} else {
		existingHooks = make(map[string]json.RawMessage)
	}

	if hooksAlreadyInstalled(existingHooks) && !hooksNeedUpdate(existingHooks) && !hasStaleHookEvents(existingHooks) {
		debuglog.Logger.Debug("claude hooks: already installed and up to date")
		return false, nil
	}

	for _, cfg := range hookEventConfigs {
		existingHooks[cfg.Event] = mergeHookEvent(existingHooks[cfg.Event], cfg.Matcher, cfg.Async)
	}

	// Clean up stale brizz-code hooks from events we no longer subscribe to.
	cleanStaleHookEvents(existingHooks)

	hooksRaw, err := json.Marshal(existingHooks)
	if err != nil {
		return false, fmt.Errorf("marshal hooks: %w", err)
	}
	rawSettings["hooks"] = hooksRaw

	finalData, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal settings: %w", err)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}

	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, finalData, 0644); err != nil {
		return false, fmt.Errorf("write settings.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		os.Remove(tmpPath)
		debuglog.Logger.Error("claude hooks: failed to rename settings.json.tmp", "err", err)
		return false, fmt.Errorf("rename settings.json: %w", err)
	}

	debuglog.Logger.Info("claude hooks injected", "path", settingsPath)
	return true, nil
}

// RemoveClaudeHooks removes brizz-code hook entries from Claude Code's settings.json.
func RemoveClaudeHooks(configDir string) (bool, error) {
	debuglog.Logger.Debug("claude hooks: removing hooks", "configDir", configDir)
	settingsPath := filepath.Join(configDir, "settings.json")

	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			debuglog.Logger.Debug("claude hooks remove: settings.json not found, nothing to remove")
			return false, nil
		}
		debuglog.Logger.Error("claude hooks remove: failed to read settings.json", "path", settingsPath, "err", err)
		return false, fmt.Errorf("read settings.json: %w", err)
	}

	var rawSettings map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawSettings); err != nil {
		debuglog.Logger.Error("claude hooks remove: failed to parse settings.json", "path", settingsPath, "err", err)
		return false, fmt.Errorf("parse settings.json: %w", err)
	}

	hooksRaw, ok := rawSettings["hooks"]
	if !ok {
		debuglog.Logger.Debug("claude hooks remove: no hooks section found")
		return false, nil
	}

	var existingHooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &existingHooks); err != nil {
		debuglog.Logger.Error("claude hooks remove: failed to parse hooks section", "err", err)
		return false, nil
	}

	removed := false
	for _, cfg := range hookEventConfigs {
		if raw, ok := existingHooks[cfg.Event]; ok {
			cleaned, didRemove := removeBrizzCodeFromEvent(raw)
			if didRemove {
				removed = true
				if cleaned == nil {
					delete(existingHooks, cfg.Event)
				} else {
					existingHooks[cfg.Event] = cleaned
				}
			}
		}
	}

	if !removed {
		debuglog.Logger.Debug("claude hooks remove: no brizz-code hooks found to remove")
		return false, nil
	}

	if len(existingHooks) == 0 {
		delete(rawSettings, "hooks")
	} else {
		hooksData, _ := json.Marshal(existingHooks)
		rawSettings["hooks"] = hooksData
	}

	finalData, err := json.MarshalIndent(rawSettings, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshal settings: %w", err)
	}

	tmpPath := settingsPath + ".tmp"
	if err := os.WriteFile(tmpPath, finalData, 0644); err != nil {
		debuglog.Logger.Error("claude hooks remove: failed to write settings.json.tmp", "err", err)
		return false, fmt.Errorf("write settings.json.tmp: %w", err)
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		os.Remove(tmpPath)
		debuglog.Logger.Error("claude hooks remove: failed to rename settings.json.tmp", "err", err)
		return false, fmt.Errorf("rename settings.json: %w", err)
	}

	debuglog.Logger.Info("claude hooks removed", "path", settingsPath)
	return true, nil
}

// AreHooksInstalled checks if brizz-code hooks are present in settings.json.
func AreHooksInstalled(configDir string) bool {
	settingsPath := filepath.Join(configDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}

	var rawSettings map[string]json.RawMessage
	if err := json.Unmarshal(data, &rawSettings); err != nil {
		return false
	}

	hooksRaw, ok := rawSettings["hooks"]
	if !ok {
		return false
	}

	var existingHooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &existingHooks); err != nil {
		return false
	}

	return hooksAlreadyInstalled(existingHooks)
}

// hasStaleHookEvents checks if there are brizz-code hooks in events we no longer subscribe to.
func hasStaleHookEvents(hooks map[string]json.RawMessage) bool {
	activeEvents := make(map[string]bool)
	for _, cfg := range hookEventConfigs {
		activeEvents[cfg.Event] = true
	}
	for event, raw := range hooks {
		if activeEvents[event] {
			continue
		}
		if eventHasBrizzCodeHook(raw) {
			return true
		}
	}
	return false
}

// cleanStaleHookEvents removes brizz-code hooks from events we no longer subscribe to.
func cleanStaleHookEvents(hooks map[string]json.RawMessage) {
	activeEvents := make(map[string]bool)
	for _, cfg := range hookEventConfigs {
		activeEvents[cfg.Event] = true
	}

	for event, raw := range hooks {
		if activeEvents[event] {
			continue
		}
		if !eventHasBrizzCodeHook(raw) {
			continue
		}
		cleaned, didRemove := removeBrizzCodeFromEvent(raw)
		if didRemove {
			if cleaned == nil {
				delete(hooks, event)
			} else {
				hooks[event] = cleaned
			}
		}
	}
}

// hooksAlreadyInstalled checks if all required brizz-code hooks are present.
func hooksAlreadyInstalled(hooks map[string]json.RawMessage) bool {
	for _, cfg := range hookEventConfigs {
		raw, ok := hooks[cfg.Event]
		if !ok {
			return false
		}
		if cfg.Matcher != "" {
			if !eventHasBrizzCodeHookWithMatcher(raw, cfg.Matcher) {
				return false
			}
		} else {
			if !eventHasBrizzCodeHook(raw) {
				return false
			}
		}
	}
	return true
}

// hooksNeedUpdate checks if the hook command path has changed (e.g., after rebuild).
func hooksNeedUpdate(hooks map[string]json.RawMessage) bool {
	currentCmd := GetHookCommand()
	for _, cfg := range hookEventConfigs {
		raw, ok := hooks[cfg.Event]
		if !ok {
			continue
		}
		var matchers []claudeHookMatcher
		if err := json.Unmarshal(raw, &matchers); err != nil {
			continue
		}
		for _, m := range matchers {
			// Only check matchers relevant to this config.
			if cfg.Matcher != "" && m.Matcher != cfg.Matcher {
				continue
			}
			for _, h := range m.Hooks {
				if strings.Contains(h.Command, brizzCodeHookMarker) && h.Command != currentCmd {
					return true
				}
			}
		}
	}
	return false
}

// eventHasBrizzCodeHookWithMatcher checks if a hook event contains our hook under a specific matcher.
func eventHasBrizzCodeHookWithMatcher(raw json.RawMessage, matcher string) bool {
	var matchers []claudeHookMatcher
	if err := json.Unmarshal(raw, &matchers); err != nil {
		return false
	}
	for _, m := range matchers {
		if m.Matcher != matcher {
			continue
		}
		for _, h := range m.Hooks {
			if strings.Contains(h.Command, brizzCodeHookMarker) {
				return true
			}
		}
	}
	return false
}

// eventHasBrizzCodeHook checks if a hook event contains our hook.
func eventHasBrizzCodeHook(raw json.RawMessage) bool {
	var matchers []claudeHookMatcher
	if err := json.Unmarshal(raw, &matchers); err != nil {
		return false
	}
	for _, m := range matchers {
		for _, h := range m.Hooks {
			if strings.Contains(h.Command, brizzCodeHookMarker) {
				return true
			}
		}
	}
	return false
}

// mergeHookEvent adds brizz-code's hook to an event's matcher array, preserving existing hooks.
func mergeHookEvent(existing json.RawMessage, matcher string, async bool) json.RawMessage {
	var matchers []claudeHookMatcher

	if existing != nil {
		if err := json.Unmarshal(existing, &matchers); err != nil {
			matchers = nil
		}
	}

	currentCmd := GetHookCommand()

	// Check if we already have a matcher entry with our hook.
	for i, m := range matchers {
		if m.Matcher == matcher {
			for j, h := range m.Hooks {
				if strings.Contains(h.Command, brizzCodeHookMarker) {
					// Update command path if changed.
					if h.Command != currentCmd {
						matchers[i].Hooks[j].Command = currentCmd
					}
					result, _ := json.Marshal(matchers)
					return result
				}
			}
			// Append our hook to existing matcher.
			matchers[i].Hooks = append(matchers[i].Hooks, brizzCodeHook(async))
			result, _ := json.Marshal(matchers)
			return result
		}
	}

	// No matching matcher found; add a new one.
	newMatcher := claudeHookMatcher{
		Matcher: matcher,
		Hooks:   []claudeHookEntry{brizzCodeHook(async)},
	}
	matchers = append(matchers, newMatcher)
	result, _ := json.Marshal(matchers)
	return result
}

// removeBrizzCodeFromEvent removes brizz-code hook entries from an event's matcher array.
func removeBrizzCodeFromEvent(raw json.RawMessage) (json.RawMessage, bool) {
	var matchers []claudeHookMatcher
	if err := json.Unmarshal(raw, &matchers); err != nil {
		return raw, false
	}

	removed := false
	var cleaned []claudeHookMatcher

	for _, m := range matchers {
		var kept []claudeHookEntry
		for _, h := range m.Hooks {
			if strings.Contains(h.Command, brizzCodeHookMarker) {
				removed = true
				continue
			}
			kept = append(kept, h)
		}
		if len(kept) > 0 {
			m.Hooks = kept
			cleaned = append(cleaned, m)
		}
	}

	if !removed {
		return raw, false
	}
	if len(cleaned) == 0 {
		return nil, true
	}

	result, _ := json.Marshal(cleaned)
	return result, true
}
