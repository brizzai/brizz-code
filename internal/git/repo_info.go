package git

import (
	"time"

	"github.com/yuvalhayke/brizz-code/internal/github"
)

// RepoInfo holds cached git and PR metadata for a repository.
type RepoInfo struct {
	Branch         string
	IsDirty        bool
	IsWorktreeRepo bool
	PR             *github.PR
	LastGitRefresh time.Time
	LastPRRefresh  time.Time
}

// RefreshGitInfo fetches branch, dirty status, and worktree info for a repo.
// Fast operation (<10ms, all local git commands).
func RefreshGitInfo(repoPath string) *RepoInfo {
	return &RepoInfo{
		Branch:         GetBranchName(repoPath),
		IsDirty:        HasUncommittedChanges(repoPath),
		IsWorktreeRepo: IsWorktree(repoPath),
		LastGitRefresh: time.Now(),
	}
}

// RefreshPRInfo fetches PR info via gh CLI and updates the RepoInfo.
// Slower operation (~200ms, network call).
func RefreshPRInfo(info *RepoInfo, repoPath string) {
	pr, _ := github.GetPRForBranch(repoPath, info.Branch)
	info.PR = pr
	info.LastPRRefresh = time.Now()
}
