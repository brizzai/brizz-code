package git

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// GetBranchName returns the current branch name for the given repo path.
// Returns empty string if not a git repo or on error.
func GetBranchName(repoPath string) string {
	cmd := exec.Command("git", "-C", repoPath, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// HasUncommittedChanges returns true if the working tree has uncommitted changes.
func HasUncommittedChanges(repoPath string) bool {
	cmd := exec.Command("git", "-C", repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

// IsWorktree returns true if the given path is a git worktree (not the main repo).
func IsWorktree(repoPath string) bool {
	gitDir := exec.Command("git", "-C", repoPath, "rev-parse", "--git-dir")
	gitDirOut, err := gitDir.Output()
	if err != nil {
		return false
	}

	commonDir := exec.Command("git", "-C", repoPath, "rev-parse", "--git-common-dir")
	commonDirOut, err := commonDir.Output()
	if err != nil {
		return false
	}

	gitDirPath := strings.TrimSpace(string(gitDirOut))
	commonDirPath := strings.TrimSpace(string(commonDirOut))

	// Resolve to absolute paths for comparison.
	if !filepath.IsAbs(gitDirPath) {
		gitDirPath = filepath.Join(repoPath, gitDirPath)
	}
	if !filepath.IsAbs(commonDirPath) {
		commonDirPath = filepath.Join(repoPath, commonDirPath)
	}

	gitDirPath = filepath.Clean(gitDirPath)
	commonDirPath = filepath.Clean(commonDirPath)

	return gitDirPath != commonDirPath
}
