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
		return RenderPanelTitle(" PREVIEW", width) + "\n" + DimStyle.Render("  No session selected")
	}

	var b strings.Builder

	// Panel title.
	b.WriteString(RenderPanelTitle(" PREVIEW", width))
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
	usedLines := 5 // panel title + underline + header + path + separator
	if gitLine := renderGitInfoLine(repoInfo); gitLine != "" {
		b.WriteString("  " + gitLine)
		b.WriteString("\n")
		usedLines++
	}

	// Workspace name.
	if s.WorkspaceName != "" {
		b.WriteString(DimStyle.Render(fmt.Sprintf("  workspace: %s", s.WorkspaceName)))
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

	// Strip OSC-8 hyperlinks to prevent dotted underlines in preview.
	content = stripOSC8(content)

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
		if pr.CIStatus == "FAILURE" {
			details = append(details, "CI failing")
		}
		if pr.ReviewDecision == "CHANGES_REQUESTED" {
			details = append(details, "changes requested")
		}
		if pr.ReviewDecision == "APPROVED" {
			details = append(details, "approved")
		}
		if pr.CIStatus == "SUCCESS" && pr.ReviewDecision != "APPROVED" {
			details = append(details, "CI passing")
		}
		if pr.CIStatus == "PENDING" {
			details = append(details, "CI pending")
		}
		if pr.ReviewDecision == "REVIEW_REQUIRED" {
			details = append(details, "review pending")
		}
		if len(details) > 0 {
			prText += " (" + strings.Join(details, ", ") + ")"
		}

		ciFail := pr.CIStatus == "FAILURE"
		changesReq := pr.ReviewDecision == "CHANGES_REQUESTED"
		approved := pr.ReviewDecision == "APPROVED"
		ciPass := pr.CIStatus == "SUCCESS"

		style := PRPendingStyle // default: yellow
		if ciFail || changesReq {
			style = PRFailStyle
		} else if approved && ciPass {
			style = PROpenStyle
		}
		parts = append(parts, style.Render(prText))
	}

	return strings.Join(parts, "  ")
}

// stripOSC8 removes OSC-8 hyperlink sequences while preserving the visible link text.
// OSC-8 format: ESC]8;params;uri ST ... visible text ... ESC]8;;ST
// where ST is BEL (\x07) or ESC\ (\x1b\x5c).
func stripOSC8(content string) string {
	if !strings.Contains(content, "\x1b]8;") {
		return content
	}

	var b strings.Builder
	b.Grow(len(content))

	i := 0
	for i < len(content) {
		// Look for ESC ] 8 ;
		if i+3 < len(content) && content[i] == '\x1b' && content[i+1] == ']' && content[i+2] == '8' && content[i+3] == ';' {
			// Skip until ST (BEL or ESC\).
			j := i + 4
			for j < len(content) {
				if content[j] == '\x07' {
					j++
					break
				}
				if content[j] == '\x1b' && j+1 < len(content) && content[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
			continue
		}
		b.WriteByte(content[i])
		i++
	}

	return b.String()
}
