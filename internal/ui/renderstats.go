package ui

import (
	"fmt"
	"strings"
	"time"
)

// RenderStats accumulates rendering-related events for diagnostics.
// Instead of logging every event (which would spam the debug log),
// counters are tracked and dumped into the bug report.
type RenderStats struct {
	// Window resize tracking.
	ResizeCount    int       // total WindowSizeMsg events received
	LastResizeTime time.Time // when the last resize happened
	LastResizeW    int       // last resize width
	LastResizeH    int       // last resize height

	// Viewport drift tracking.
	ViewportDriftCount int // times viewOffset changed in syncViewport

	// Height mismatch tracking.
	HeightMismatchCount int // total View() calls where output lines != h.height
	LastMismatchDiff    int // last observed diff (output_lines - expected)
}

// RecordResize records a WindowSizeMsg event.
func (rs *RenderStats) RecordResize(w, h int) {
	rs.ResizeCount++
	rs.LastResizeTime = time.Now()
	rs.LastResizeW = w
	rs.LastResizeH = h
}

// RecordViewportDrift records a viewOffset change in syncViewport.
func (rs *RenderStats) RecordViewportDrift() {
	rs.ViewportDriftCount++
}

// RecordHeightMismatch records a View() height mismatch.
func (rs *RenderStats) RecordHeightMismatch(diff int) {
	rs.HeightMismatchCount++
	rs.LastMismatchDiff = diff
}

// FormatMarkdown formats the render stats for the bug report.
func (rs *RenderStats) FormatMarkdown(uptime time.Duration) string {
	var b strings.Builder

	b.WriteString("### Rendering Stats\n")
	fmt.Fprintf(&b, "- **Uptime**: %s\n", formatDuration(uptime))
	fmt.Fprintf(&b, "- **Window resizes**: %d", rs.ResizeCount)
	if rs.ResizeCount > 0 {
		ago := time.Since(rs.LastResizeTime)
		fmt.Fprintf(&b, " (last: %dx%d, %s ago)", rs.LastResizeW, rs.LastResizeH, formatDuration(ago))
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "- **Viewport offset changes**: %d\n", rs.ViewportDriftCount)
	fmt.Fprintf(&b, "- **View height mismatches**: %d", rs.HeightMismatchCount)
	if rs.HeightMismatchCount > 0 {
		fmt.Fprintf(&b, " (last diff: %+d lines)", rs.LastMismatchDiff)
	}
	b.WriteString("\n")

	// Flag suspicious patterns.
	if uptime > 0 {
		resizesPerMin := float64(rs.ResizeCount) / uptime.Minutes()
		if resizesPerMin > 10 {
			fmt.Fprintf(&b, "- **WARNING**: %.0f resizes/min (expected <1)\n", resizesPerMin)
		}
	}
	if rs.HeightMismatchCount > 0 {
		b.WriteString("- **WARNING**: View height mismatches detected — likely cause of scroll drift\n")
	}

	return b.String()
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
