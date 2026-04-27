package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// ResolveProvider loads workspace config from repoPath. Prefers .fleet.json /
// .fleet.local.json; falls back to legacy .bc.json / .bc.local.json so existing
// repos keep working. Local overrides base field-by-field. Returns ShellProvider
// if any command is set, otherwise GitWorktreeProvider.
func ResolveProvider(repoPath string) Provider {
	base := firstNonEmpty(
		loadRepoConfig(filepath.Join(repoPath, ".fleet.json")),
		loadRepoConfig(filepath.Join(repoPath, ".bc.json")),
	)
	local := firstNonEmpty(
		loadRepoConfig(filepath.Join(repoPath, ".fleet.local.json")),
		loadRepoConfig(filepath.Join(repoPath, ".bc.local.json")),
	)

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

func loadRepoConfig(path string) ShellConfig {
	data, err := os.ReadFile(path)
	if err != nil {
		return ShellConfig{}
	}
	var cfg RepoWorkspaceConfig
	_ = json.Unmarshal(data, &cfg)
	return cfg.Workspace
}

// firstNonEmpty returns the first ShellConfig that has any field set.
func firstNonEmpty(configs ...ShellConfig) ShellConfig {
	for _, c := range configs {
		if c.List != "" || c.Create != "" || c.Destroy != "" {
			return c
		}
	}
	return ShellConfig{}
}
