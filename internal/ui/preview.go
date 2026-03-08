package ui

import (
	"fmt"
	"strings"

	"github.com/yuvalhayke/brizz-code/internal/git"
	"github.com/yuvalhayke/brizz-code/internal/session"
)

// RenderPreview renders the preview pane for the selected session.
func RenderPreview(s *session.Session, content string, repoInfo *git.RepoInfo, width, height int) string {
	if s == nil {
		return PanelTitleStyle.Render(" PREVIEW") + "\n" + DimStyle.Render("  No session selected")
	}

	var b strings.Builder

	// Panel title.
	b.WriteString(PanelTitleStyle.Render(" PREVIEW"))
	b.WriteString("\n")

	// Header: title + status.
	header := fmt.Sprintf("  %s %s  %s",
		StatusSymbol(s.GetStatus()),
		PreviewHeaderStyle.Render(s.Title),
		StatusLabel(s.GetStatus()),
	)
	b.WriteString(header)
	b.WriteString("\n")

	// Metadata.
	b.WriteString(DimStyle.Render(fmt.Sprintf("  %s", s.ProjectPath)))
	b.WriteString("\n")

	// Git info line.
	usedLines := 4 // panel title + header + path + separator
	if gitLine := renderGitInfoLine(repoInfo); gitLine != "" {
		b.WriteString("  " + gitLine)
		b.WriteString("\n")
		usedLines++
	}

	// Separator.
	sep := strings.Repeat("─", width-2)
	if len(sep) > 0 {
		b.WriteString(DimStyle.Render("  " + sep))
		b.WriteString("\n")
	}

	// Terminal content.
	contentHeight := height - usedLines
	if contentHeight < 1 {
		contentHeight = 1
	}

	if content == "" {
		if s.GetStatus() == session.StatusError {
			b.WriteString(ErrorStyle.Render("  Session is not running"))
		} else {
			b.WriteString(DimStyle.Render("  Waiting for output..."))
		}
		return b.String()
	}

	// Show last N lines that fit.
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	start := len(lines) - contentHeight
	if start < 0 {
		start = 0
	}

	for i := start; i < len(lines); i++ {
		line := lines[i]
		// Truncate long lines.
		if len(line) > width-2 {
			line = line[:width-2]
		}
		b.WriteString("  " + line)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

// renderGitInfoLine renders a line with branch, dirty status, and PR info.
func renderGitInfoLine(info *git.RepoInfo) string {
	if info == nil || info.Branch == "" {
		return ""
	}

	var parts []string

	// Branch.
	parts = append(parts, BranchStyle.Render(branchIcon+" "+info.Branch))

	// Dirty indicator.
	if info.IsDirty {
		parts = append(parts, DirtyStyle.Render("* uncommitted"))
	}

	// PR info.
	if info.PR != nil {
		pr := info.PR
		prText := fmt.Sprintf("PR #%d", pr.Number)
		var details []string
		if pr.ReviewDecision != "" {
			switch pr.ReviewDecision {
			case "APPROVED":
				details = append(details, "approved")
			case "CHANGES_REQUESTED":
				details = append(details, "changes requested")
			case "REVIEW_REQUIRED":
				details = append(details, "review pending")
			}
		}
		if pr.CIStatus != "" {
			switch pr.CIStatus {
			case "SUCCESS":
				details = append(details, "CI passing")
			case "FAILURE":
				details = append(details, "CI failing")
			case "PENDING":
				details = append(details, "CI pending")
			}
		}
		if len(details) > 0 {
			prText += " (" + strings.Join(details, ", ") + ")"
		}

		style := PROpenStyle
		if pr.ReviewDecision == "CHANGES_REQUESTED" || pr.CIStatus == "FAILURE" {
			style = PRFailStyle
		} else if pr.CIStatus == "PENDING" || pr.ReviewDecision == "REVIEW_REQUIRED" {
			style = PRPendingStyle
		}
		parts = append(parts, style.Render(prText))
	}

	return strings.Join(parts, "  ")
}
