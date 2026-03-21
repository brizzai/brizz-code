package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// RepoWorkspaceConfig is the structure of .bc.json files.
type RepoWorkspaceConfig struct {
	Workspace ShellConfig `json:"workspace"`
}

// ShellConfig holds shell command configuration for workspace operations.
type ShellConfig struct {
	List    string `json:"list,omitempty"`
	Create  string `json:"create,omitempty"`
	Destroy string `json:"destroy,omitempty"`
}

// ResolveProvider loads .bc.json + .bc.local.json from repoPath,
// merges (local overrides base field-by-field), returns ShellProvider
// if any command is set, otherwise returns GitWorktreeProvider.
func ResolveProvider(repoPath string) Provider {
	base := loadRepoConfig(filepath.Join(repoPath, ".bc.json"))
	local := loadRepoConfig(filepath.Join(repoPath, ".bc.local.json"))

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
