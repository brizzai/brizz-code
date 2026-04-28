package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/brizzai/fleet/internal/debuglog"
)

// RepoWorkspaceConfig is the structure of .fleet.json files.
type RepoWorkspaceConfig struct {
	Workspace ShellConfig `json:"workspace"`
}

// ShellConfig holds shell command configuration for workspace operations.
type ShellConfig struct {
	List    string `json:"list,omitempty"`
	Create  string `json:"create,omitempty"`
	Destroy string `json:"destroy,omitempty"`
}

// ResolveProvider loads workspace config from repoPath. Preference is by file
// presence, not contents: if .fleet.json exists it wins (even when empty —
// that's how a user disables a stale legacy .bc.json without deleting it);
// otherwise .bc.json is used. Same rule for .fleet.local.json over
// .bc.local.json. Local overrides base field-by-field. Returns ShellProvider
// if any command ends up set, otherwise GitWorktreeProvider.
func ResolveProvider(repoPath string) Provider {
	base := preferredConfig(repoPath, ".fleet.json", ".bc.json")
	local := preferredConfig(repoPath, ".fleet.local.json", ".bc.local.json")

	// Merge: local overrides base field-by-field.
	merged := base
	if local.List != "" {
		merged.List = local.List
	}
	if local.Create != "" {
		merged.Create = local.Create
	}
	if local.Destroy != "" {
		merged.Destroy = local.Destroy
	}

	// If any shell command is set, use ShellProvider.
	if merged.List != "" || merged.Create != "" || merged.Destroy != "" {
		return &ShellProvider{
			ListCmd:    merged.List,
			CreateCmd:  merged.Create,
			DestroyCmd: merged.Destroy,
		}
	}

	// Default: built-in git worktree provider.
	return &GitWorktreeProvider{}
}

// preferredConfig returns the config from preferredName if that file exists at
// repoPath, otherwise the config from legacyName. File presence is the signal —
// an empty preferred file still suppresses the legacy one (intended way to
// "disable" a stale legacy config without deleting it).
func preferredConfig(repoPath, preferredName, legacyName string) ShellConfig {
	if cfg, ok := loadRepoConfig(filepath.Join(repoPath, preferredName)); ok {
		return cfg
	}
	cfg, _ := loadRepoConfig(filepath.Join(repoPath, legacyName))
	return cfg
}

// loadRepoConfig reads and parses a repo workspace config file. Returns the
// parsed config and true when the file exists and parses; returns the zero
// config and false when the file is missing. A file that exists but fails to
// parse is logged and treated as "exists with empty config" (true) — the user's
// intent to override is honored even if their JSON is wrong, and the warning
// surfaces the parse failure in debug.log.
func loadRepoConfig(path string) (ShellConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ShellConfig{}, false
	}
	var cfg RepoWorkspaceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		debuglog.Logger.Warn("workspace: failed to parse repo config; treating as empty override",
			"path", path, "err", err)
		return ShellConfig{}, true
	}
	return cfg.Workspace, true
}
