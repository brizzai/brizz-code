package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/diagnostics"
)

// bugReportClosedMsg is sent when the bug report dialog closes.
type bugReportClosedMsg struct{}

// BugReportDialog displays diagnostics and recent errors for bug reporting.
type BugReportDialog struct {
	visible bool
	width   int
	height  int
	scroll  int // scroll offset for content

	report       *diagnostics.Report
	errorEntries []ErrorEntry
	actionEntries []ActionEntry
	contentLines int // total rendered content lines
}

// NewBugReportDialog creates a bug report dialog.
func NewBugReportDialog() *BugReportDialog {
	return &BugReportDialog{}
}

// Show collects diagnostics and shows the dialog.
func (d *BugReportDialog) Show(version string, sessionCount int, errors *ErrorHistory, actions *ActionLog) {
	d.visible = true
	d.scroll = 0

	d.report = diagnostics.Collect(version, sessionCount)
	d.errorEntries = errors.Entries()
	d.actionEntries = actions.Entries()

	// Pre-format errors and actions into the report.
	d.report.RecentErrors = d.formatErrors()
	d.report.RecentActions = d.formatActions()
}

func (d *BugReportDialog) Hide()          { d.visible = false }
func (d *BugReportDialog) IsVisible() bool { return d.visible }
func (d *BugReportDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
}

// Update handles key events for the bug report dialog.
func (d *BugReportDialog) Update(msg tea.Msg) (*BugReportDialog, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	switch keyMsg.String() {
	case "j", "down":
		if d.scroll < d.contentLines-1 {
			d.scroll++
		}
	case "k", "up":
		if d.scroll > 0 {
			d.scroll--
		}
	case "g":
		return d, d.openGitHubIssue()
	case "esc", "q":
		d.Hide()
		return d, func() tea.Msg { return bugReportClosedMsg{} }
	}

	return d, nil
}

func (d *BugReportDialog) openGitHubIssue() tea.Cmd {
	if _, err := exec.LookPath("gh"); err != nil {
		return nil
	}

	body := d.report.FormatMarkdown()

	return func() tea.Msg {
		// Write body to temp file to avoid shell escaping issues.
		tmpFile, err := os.CreateTemp("", "brizz-bug-*.md")
		if err != nil {
			return bugReportClosedMsg{}
		}
		defer os.Remove(tmpFile.Name())

		if _, err := tmpFile.WriteString(body); err != nil {
			tmpFile.Close()
			return bugReportClosedMsg{}
		}
		tmpFile.Close()

		cmd := exec.Command("gh", "issue", "create",
			"--repo", "brizzai/brizz-code",
			"--web",
			"--body-file", tmpFile.Name(),
		)
		_ = cmd.Run()
		return bugReportClosedMsg{}
	}
}

// View renders the bug report dialog.
func (d *BugReportDialog) View() string {
	dialogWidth := 60
	if dialogWidth > d.width-4 {
		dialogWidth = d.width - 4
	}
	if dialogWidth < 40 {
		dialogWidth = 40
	}
	innerWidth := dialogWidth - 6 // padding

	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorText)
	dimStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	errorStyle := lipgloss.NewStyle().Foreground(ColorRed)

	// Title.
	b.WriteString(titleStyle.Render("Bug Report"))
	b.WriteString("\n")

	// Recent Errors.
	b.WriteString("\n")
	errCount := len(d.errorEntries)
	if errCount > 10 {
		errCount = 10
	}
	b.WriteString(sectionStyle.Render(fmt.Sprintf("Recent Errors (%d)", len(d.errorEntries))))
	b.WriteString("\n")
	if len(d.errorEntries) == 0 {
		b.WriteString(dimStyle.Render("  No errors recorded"))
		b.WriteString("\n")
	} else {
		for i := 0; i < errCount; i++ {
			e := d.errorEntries[i]
			ago := formatTimeAgo(e.Timestamp)
			line := fmt.Sprintf("  %s  %s", dimStyle.Render(ago), errorStyle.Render(truncate(e.Message, innerWidth-12)))
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Recent Actions.
	b.WriteString("\n")
	actionCount := len(d.actionEntries)
	if actionCount > 10 {
		actionCount = 10
	}
	b.WriteString(sectionStyle.Render("Recent Actions"))
	b.WriteString("\n")
	if len(d.actionEntries) == 0 {
		b.WriteString(dimStyle.Render("  No actions recorded"))
		b.WriteString("\n")
	} else {
		for i := 0; i < actionCount; i++ {
			a := d.actionEntries[i]
			ago := formatTimeAgo(a.Timestamp)
			result := dimStyle.Render("ok")
			if !a.Success {
				result = errorStyle.Render("ERROR")
			}
			detail := truncate(a.Detail, innerWidth-35)
			line := fmt.Sprintf("  %s  %-18s %-20s %s",
				dimStyle.Render(ago),
				a.Action,
				dimStyle.Render(detail),
				result,
			)
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Diagnostics summary.
	b.WriteString("\n")
	b.WriteString(sectionStyle.Render("Diagnostics"))
	b.WriteString("\n")
	r := d.report
	diag := fmt.Sprintf("  %s", r.Version)
	if r.MacOSVersion != "" {
		diag += fmt.Sprintf(" · macOS %s", r.MacOSVersion)
	}
	diag += fmt.Sprintf(" · %s", r.Arch)
	if r.TmuxVersion != "" {
		diag += fmt.Sprintf(" · %s", r.TmuxVersion)
	}
	b.WriteString(dimStyle.Render(diag))
	b.WriteString("\n")

	// Divider.
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", innerWidth)))
	b.WriteString("\n")

	// Controls.
	ghAvailable := true
	if _, err := exec.LookPath("gh"); err != nil {
		ghAvailable = false
	}
	if ghAvailable {
		b.WriteString(dimStyle.Render("g") + " Open GitHub issue    " + dimStyle.Render("esc") + " Close")
	} else {
		b.WriteString(dimStyle.Render("gh CLI not found") + "    " + dimStyle.Render("esc") + " Close")
	}

	content := b.String()
	d.contentLines = strings.Count(content, "\n") + 1

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(ColorAccent).
		Padding(1, 2).
		Width(dialogWidth)

	box := boxStyle.Render(content)
	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, box)
}

func (d *BugReportDialog) formatErrors() []string {
	home, _ := os.UserHomeDir()
	var result []string
	for _, e := range d.errorEntries {
		ago := formatTimeAgo(e.Timestamp)
		msg := e.Message
		if home != "" {
			msg = strings.ReplaceAll(msg, home, "~")
		}
		result = append(result, fmt.Sprintf("%s | %s", ago, msg))
	}
	return result
}

func (d *BugReportDialog) formatActions() []string {
	home, _ := os.UserHomeDir()
	var result []string
	count := len(d.actionEntries)
	if count > 20 {
		count = 20
	}
	for i := 0; i < count; i++ {
		a := d.actionEntries[i]
		ts := a.Timestamp.Format("15:04:05")
		detail := a.Detail
		if home != "" {
			detail = strings.ReplaceAll(detail, home, "~")
		}
		result_ := "ok"
		if !a.Success {
			result_ = "ERROR"
		}
		result = append(result, fmt.Sprintf("%s | %s | %s | %s", ts, a.Action, detail, result_))
	}
	return result
}

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
