package github

import (
	"encoding/json"
	"os/exec"
	"strings"
)

// PR represents a GitHub pull request.
type PR struct {
	Number         int
	Title          string
	URL            string
	State          string // OPEN, CLOSED, MERGED
	ReviewDecision string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, ""
	CIStatus       string // SUCCESS, FAILURE, PENDING, ""
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
		"-R", repoPath,
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
		Number:         resp.Number,
		Title:          resp.Title,
		URL:            resp.URL,
		State:          resp.State,
		ReviewDecision: resp.ReviewDecision,
		CIStatus:       deriveCIStatus(resp.StatusCheckRollup),
	}

	return pr, nil
}

// deriveCIStatus determines overall CI status from status check rollup.
func deriveCIStatus(checks []statusCheckEntry) string {
	if len(checks) == 0 {
		return ""
	}

	hasFailure := false
	hasPending := false

	for _, check := range checks {
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
