package workspace

import (
	"path/filepath"
	"testing"
)

func TestParseWorktreePorcelain(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		want   int // expected count
		checks func(t *testing.T, result []WorkspaceInfo)
	}{
		{
			"single worktree",
			"worktree /code/myrepo\nbranch refs/heads/main\n\n",
			1,
			func(t *testing.T, result []WorkspaceInfo) {
				if result[0].Path != "/code/myrepo" {
					t.Errorf("path = %q, want /code/myrepo", result[0].Path)
				}
				if result[0].Branch != "main" {
					t.Errorf("branch = %q, want main", result[0].Branch)
				}
				if result[0].Name != "myrepo" {
					t.Errorf("name = %q, want myrepo", result[0].Name)
				}
			},
		},
		{
			"multiple worktrees",
			"worktree /code/myrepo\nbranch refs/heads/main\n\nworktree /code/myrepo-feat\nbranch refs/heads/feat\n\n",
			2,
			func(t *testing.T, result []WorkspaceInfo) {
				if result[0].Branch != "main" {
					t.Errorf("first branch = %q, want main", result[0].Branch)
				}
				if result[1].Branch != "feat" {
					t.Errorf("second branch = %q, want feat", result[1].Branch)
				}
			},
		},
		{
			"no branch (detached HEAD)",
			"worktree /code/myrepo\nHEAD abc123\ndetached\n\n",
			1,
			func(t *testing.T, result []WorkspaceInfo) {
				if result[0].Branch != "" {
					t.Errorf("branch = %q, want empty", result[0].Branch)
				}
			},
		},
		{"empty input", "", 0, nil},
		{"whitespace only", "\n\n", 0, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseWorktreePorcelain(tt.input)
			if len(result) != tt.want {
				t.Fatalf("len(result) = %d, want %d", len(result), tt.want)
			}
			if tt.checks != nil {
				tt.checks(t, result)
			}
		})
	}
}

func TestSanitizeBranchName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"slashes", "feature/login", "feature-login"},
		{"spaces", "my branch", "my-branch"},
		{"double dots", "v1..v2", "v1-v2"},
		{"clean name", "fix-bug-123", "fix-bug-123"},
		{"multiple slashes", "user/feature/thing", "user-feature-thing"},
		{"mixed", "feat/my branch..v2", "feat-my-branch-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeBranchName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeBranchName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDeriveWorktreePath(t *testing.T) {
	tests := []struct {
		name     string
		repoPath string
		wtName   string
		wantEnd  string // suffix the result should end with
	}{
		{"basic", "/code/myrepo", "feature-login", "myrepo-feature-login"},
		{"nested repo", "/home/user/projects/app", "hotfix", "app-hotfix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveWorktreePath(tt.repoPath, tt.wtName)
			if filepath.Base(got) != tt.wantEnd {
				t.Errorf("deriveWorktreePath(%q, %q) base = %q, want %q", tt.repoPath, tt.wtName, filepath.Base(got), tt.wantEnd)
			}
			// Should be a sibling directory (same parent).
			if filepath.Dir(got) != filepath.Dir(tt.repoPath) {
				t.Errorf("deriveWorktreePath result parent = %q, want %q", filepath.Dir(got), filepath.Dir(tt.repoPath))
			}
		})
	}
}
