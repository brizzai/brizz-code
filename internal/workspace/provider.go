package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
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

// Provider wraps external workspace management commands.
type Provider struct {
	ListCmd    string // shell command, returns JSON array of WorkspaceInfo
	CreateCmd  string // shell command with {{name}}, {{branch}} placeholders
	DestroyCmd string // shell command with {{name}} placeholder
}

// IsConfigured returns true if any command is set.
func (p *Provider) IsConfigured() bool {
	return p.ListCmd != "" || p.CreateCmd != "" || p.DestroyCmd != ""
}

// CanList returns true if the list command is configured.
func (p *Provider) CanList() bool { return p.ListCmd != "" }

// CanCreate returns true if the create command is configured.
func (p *Provider) CanCreate() bool { return p.CreateCmd != "" }

// CanDestroy returns true if the destroy command is configured.
func (p *Provider) CanDestroy() bool { return p.DestroyCmd != "" }

// List runs the list command and returns discovered workspaces.
func (p *Provider) List() ([]WorkspaceInfo, error) {
	if p.ListCmd == "" {
		return nil, fmt.Errorf("list command not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", p.ListCmd)
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

// Create runs the create command with the given name and branch.
func (p *Provider) Create(name, branch string) (*WorkspaceInfo, error) {
	if p.CreateCmd == "" {
		return nil, fmt.Errorf("create command not configured")
	}

	cmdStr := strings.ReplaceAll(p.CreateCmd, "{{name}}", name)
	cmdStr = strings.ReplaceAll(cmdStr, "{{branch}}", branch)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("create command failed: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("create command failed: %w", err)
	}

	var info WorkspaceInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse create output: %w", err)
	}
	return &info, nil
}

// Destroy runs the destroy command for the given workspace name.
func (p *Provider) Destroy(name string) error {
	if p.DestroyCmd == "" {
		return fmt.Errorf("destroy command not configured")
	}

	cmdStr := strings.ReplaceAll(p.DestroyCmd, "{{name}}", name)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("destroy command failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}
