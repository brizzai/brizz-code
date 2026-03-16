package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WorkspaceInfo represents a workspace from the provider.
type WorkspaceInfo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Branch string `json:"branch,omitempty"`
	Status string `json:"status,omitempty"`
}

// Provider is the interface for workspace operations.
type Provider interface {
	List(repoPath string) ([]WorkspaceInfo, error)
	Create(repoPath, name, branch, baseBranch string) (*WorkspaceInfo, error)
	Destroy(repoPath, name string) error
	CanCreate() bool
	CanDestroy() bool
	IsCustom() bool
}

// --- GitWorktreeProvider (built-in default) ---

// GitWorktreeProvider uses git worktree commands for workspace management.
type GitWorktreeProvider struct{}

func (g *GitWorktreeProvider) List(repoPath string) ([]WorkspaceInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git worktree list: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git worktree list: %w", err)
	}

	all := parseWorktreePorcelain(string(out))

	// Filter out the main worktree (where path == repoPath).
	absRepo, _ := filepath.Abs(repoPath)
	var result []WorkspaceInfo
	for _, ws := range all {
		absWS, _ := filepath.Abs(ws.Path)
		if absWS == absRepo {
			continue
		}
		result = append(result, ws)
	}
	return result, nil
}

func (g *GitWorktreeProvider) Create(repoPath, name, branch, baseBranch string) (*WorkspaceInfo, error) {
	path := deriveWorktreePath(repoPath, name)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Build args: git worktree add <path> -b <branch> [<start-point>]
	args := []string{"-C", repoPath, "worktree", "add", path, "-b", branch}
	if baseBranch != "" {
		args = append(args, baseBranch)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Branch might already exist — retry without -b.
		args2 := []string{"-C", repoPath, "worktree", "add", path, branch}
		cmd2 := exec.CommandContext(ctx, "git", args2...)
		if out2, err2 := cmd2.CombinedOutput(); err2 != nil {
			// Return the more informative error.
			errMsg := strings.TrimSpace(string(out))
			if errMsg == "" {
				errMsg = strings.TrimSpace(string(out2))
			}
			return nil, fmt.Errorf("git worktree add: %s", errMsg)
		}
	}

	return &WorkspaceInfo{
		Name:   name,
		Path:   path,
		Branch: branch,
	}, nil
}

func (g *GitWorktreeProvider) Destroy(repoPath, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// List ALL worktrees (unfiltered) — repoPath might be a linked worktree
	// itself, so we can't use g.List() which filters out the repoPath entry.
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "list", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("git worktree list: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return fmt.Errorf("git worktree list: %w", err)
	}
	all := parseWorktreePorcelain(string(out))
	if len(all) == 0 {
		return fmt.Errorf("worktree %q not found", name)
	}

	// First entry is always the main worktree — use it as base for derived path
	// and as the -C target for git worktree remove.
	mainPath, _ := filepath.Abs(all[0].Path)
	absRepo, _ := filepath.Abs(repoPath)
	derivedPath, _ := filepath.Abs(deriveWorktreePath(mainPath, name))

	// Find the worktree to remove. Try multiple matching strategies:
	// 1. Exact name match (ws.Name == name)
	// 2. Derived path match (worktree created by brizz-code: mainRepo-name)
	// 3. repoPath itself is the worktree (caller resolved GetRepoRoot to worktree path)
	var wtPath string
	for _, ws := range all {
		absWS, _ := filepath.Abs(ws.Path)
		if absWS == mainPath {
			continue // never remove the main worktree
		}
		if ws.Name == name || absWS == derivedPath || absWS == absRepo {
			wtPath = ws.Path
			break
		}
	}
	if wtPath == "" {
		return fmt.Errorf("worktree %q not found", name)
	}

	cmd = exec.CommandContext(ctx, "git", "-C", mainPath, "worktree", "remove", "--force", wtPath)
	if rmOut, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove: %s", strings.TrimSpace(string(rmOut)))
	}
	return nil
}

func (g *GitWorktreeProvider) CanCreate() bool  { return true }
func (g *GitWorktreeProvider) CanDestroy() bool { return true }
func (g *GitWorktreeProvider) IsCustom() bool   { return false }

// --- ShellProvider (from .bc.json) ---

// ShellProvider wraps external shell commands for workspace management.
type ShellProvider struct {
	ListCmd    string
	CreateCmd  string
	DestroyCmd string
}

func (p *ShellProvider) List(repoPath string) ([]WorkspaceInfo, error) {
	if p.ListCmd == "" {
		return nil, fmt.Errorf("list command not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.ListCmd)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("list command failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("list command failed: %w", err)
	}

	var workspaces []WorkspaceInfo
	if err := json.Unmarshal(out, &workspaces); err != nil {
		return nil, fmt.Errorf("parse list output: %w", err)
	}
	return workspaces, nil
}

func (p *ShellProvider) Create(repoPath, name, branch, baseBranch string) (*WorkspaceInfo, error) {
	if p.CreateCmd == "" {
		return nil, fmt.Errorf("create command not configured")
	}

	cmdStr := strings.ReplaceAll(p.CreateCmd, "{{name}}", name)
	if branch == "" {
		// Strip --branch {{branch}} / -b {{branch}} patterns when branch is empty.
		cmdStr = strings.ReplaceAll(cmdStr, "--branch {{branch}}", "")
		cmdStr = strings.ReplaceAll(cmdStr, "-b {{branch}}", "")
		cmdStr = strings.ReplaceAll(cmdStr, "{{branch}}", "")
	} else {
		cmdStr = strings.ReplaceAll(cmdStr, "{{branch}}", branch)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = repoPath
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("create command failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("create command failed: %w", err)
	}

	// Try parsing output as JSON first.
	var info WorkspaceInfo
	if err := json.Unmarshal(out, &info); err == nil {
		return &info, nil
	}

	// Output wasn't JSON — look up the new workspace via list command.
	if p.ListCmd != "" {
		workspaces, listErr := p.List(repoPath)
		if listErr == nil {
			for _, ws := range workspaces {
				if ws.Name == name {
					return &ws, nil
				}
			}
		}
	}

	// Fall back to name-only info.
	return &WorkspaceInfo{Name: name, Branch: branch}, nil
}

func (p *ShellProvider) Destroy(repoPath, name string) error {
	if p.DestroyCmd == "" {
		return fmt.Errorf("destroy command not configured")
	}

	cmdStr := strings.ReplaceAll(p.DestroyCmd, "{{name}}", name)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = repoPath
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("destroy command failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func (p *ShellProvider) CanCreate() bool  { return p.CreateCmd != "" }
func (p *ShellProvider) CanDestroy() bool { return p.DestroyCmd != "" }
func (p *ShellProvider) IsCustom() bool   { return true }

// --- Helper functions ---

// parseWorktreePorcelain parses git worktree list --porcelain output.
func parseWorktreePorcelain(output string) []WorkspaceInfo {
	var result []WorkspaceInfo
	var current WorkspaceInfo
	hasWorktree := false

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "worktree ") {
			if hasWorktree {
				result = append(result, current)
			}
			path := strings.TrimPrefix(line, "worktree ")
			current = WorkspaceInfo{
				Path: path,
				Name: filepath.Base(path),
			}
			hasWorktree = true
		} else if strings.HasPrefix(line, "branch refs/heads/") {
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if hasWorktree {
		result = append(result, current)
	}
	return result
}

// SanitizeBranchName converts a branch name to a safe directory name.
// e.g. "feature/login" -> "feature-login"
func SanitizeBranchName(branch string) string {
	s := strings.ReplaceAll(branch, "/", "-")
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "..", "-")
	return s
}

// deriveWorktreePath computes the sibling worktree path.
// e.g. repoPath="/code/myrepo", name="feature-login" -> "/code/myrepo-feature-login"
func deriveWorktreePath(repoPath, name string) string {
	absRepo, _ := filepath.Abs(repoPath)
	parent := filepath.Dir(absRepo)
	base := filepath.Base(absRepo)
	return filepath.Join(parent, base+"-"+name)
}

// DeriveWorktreePathPreview returns a display-friendly relative path preview.
func DeriveWorktreePathPreview(repoPath, name string) string {
	absRepo, _ := filepath.Abs(repoPath)
	base := filepath.Base(absRepo)
	return "../" + base + "-" + name
}
