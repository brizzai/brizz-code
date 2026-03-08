package ui

import (
	"fmt"
	"strings"

	"github.com/yuvalhayke/brizz-code/internal/session"
)

// RenderPreview renders the preview pane for the selected session.
func RenderPreview(s *session.Session, content string, width, height int) string {
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

	// Separator.
	sep := strings.Repeat("─", width-2)
	if len(sep) > 0 {
		b.WriteString(DimStyle.Render("  " + sep))
		b.WriteString("\n")
	}

	// Terminal content.
	usedLines := 4 // panel title + header + path + separator
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
