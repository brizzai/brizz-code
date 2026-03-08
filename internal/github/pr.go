package github

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// PR represents a GitHub pull request.
type PR struct {
	Number            int
	Title             string
	URL               string
	State             string // OPEN, CLOSED, MERGED
	ReviewDecision    string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	CIStatus          string // SUCCESS, FAILURE, PENDING, ""
	UnresolvedThreads int    // count of unresolved review threads
}

// IsGHAvailable checks if the gh CLI is installed and accessible.
func IsGHAvailable() bool {
	cmd := exec.Command("gh", "--version")
	return cmd.Run() == nil
}

// ghPRResponse matches the JSON output of gh pr view.
type ghPRResponse struct {
	Number              int                `json:"number"`
	Title               string             `json:"title"`
	URL                 string             `json:"url"`
	State               string             `json:"state"`
	ReviewDecision      string             `json:"reviewDecision"`
	StatusCheckRollup   []statusCheckEntry `json:"statusCheckRollup"`
}

type statusCheckEntry struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}

// GetPRForBranch returns the PR associated with the current branch, or nil if none.
func GetPRForBranch(repoPath, branch string) (*PR, error) {
	if branch == "" || branch == "HEAD" {
		return nil, nil
	}

	cmd := exec.Command("gh", "pr", "view",
		"--json", "number,title,url,state,reviewDecision,statusCheckRollup",
	)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		// gh returns exit code 1 when no PR exists for the branch.
		return nil, nil
	}

	var resp ghPRResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return nil, nil
	}

	pr := &PR{
		Number:            resp.Number,
		Title:             resp.Title,
		URL:               resp.URL,
		State:             resp.State,
		ReviewDecision:    resp.ReviewDecision,
		CIStatus:          deriveCIStatus(resp.StatusCheckRollup),
		UnresolvedThreads: getUnresolvedThreadCount(repoPath, resp.Number, resp.URL),
	}

	return pr, nil
}

// getUnresolvedThreadCount queries GitHub GraphQL API for unresolved review thread count.
func getUnresolvedThreadCount(repoPath string, prNumber int, prURL string) int {
	// Parse owner/repo from PR URL: https://github.com/owner/repo/pull/123
	trimmed := strings.TrimPrefix(prURL, "https://github.com/")
	parts := strings.Split(trimmed, "/")
	if len(parts) < 3 {
		return 0
	}
	owner, repo := parts[0], parts[1]

	query := fmt.Sprintf(`query {
		repository(owner: "%s", name: "%s") {
			pullRequest(number: %d) {
				reviewThreads(first: 100) {
					nodes { isResolved }
				}
			}
		}
	}`, owner, repo, prNumber)

	cmd := exec.Command("gh", "api", "graphql", "-f", "query="+query)
	cmd.Dir = repoPath
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	var result struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return 0
	}

	count := 0
	for _, t := range result.Data.Repository.PullRequest.ReviewThreads.Nodes {
		if !t.IsResolved {
			count++
		}
	}
	return count
}

// deriveCIStatus determines overall CI status from status check rollup.
func deriveCIStatus(checks []statusCheckEntry) string {
	if len(checks) == 0 {
		return ""
	}

	hasFailure := false
	hasPending := false

	for _, check := range checks {
		// Skip ghost entries with no name (null checks from GitHub API).
		if check.Name == "" {
			continue
		}
		conclusion := strings.ToUpper(check.Conclusion)
		status := strings.ToUpper(check.Status)

		if conclusion == "FAILURE" || conclusion == "ERROR" || conclusion == "TIMED_OUT" {
			hasFailure = true
		} else if status == "IN_PROGRESS" || status == "QUEUED" || status == "PENDING" || conclusion == "" {
			hasPending = true
		}
	}

	if hasFailure {
		return "FAILURE"
	}
	if hasPending {
		return "PENDING"
	}
	return "SUCCESS"
}
