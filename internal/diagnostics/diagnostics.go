package diagnostics

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"os/exec"
)

// Report holds collected diagnostic information.
type Report struct {
	Version       string
	GoVersion     string
	OS            string
	Arch          string
	MacOSVersion  string
	TmuxVersion   string
	ClaudeVersion string
	GhVersion     string
	Config        string
	SessionCount  int
	RecentErrors  []string // pre-formatted from ErrorHistory
	RecentActions []string // pre-formatted from ActionLog
	RecentLogs    string   // last 100 lines of debug.log
}

// Collect gathers system diagnostics.
func Collect(version string, sessionCount int) *Report {
	r := &Report{
		Version:      version,
		GoVersion:    runtime.Version(),
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		SessionCount: sessionCount,
	}

	r.MacOSVersion = runCmd("sw_vers", "-productVersion")
	r.TmuxVersion = runCmd("tmux", "-V")
	r.ClaudeVersion = runCmd("claude", "--version")
	r.GhVersion = firstLine(runCmd("gh", "--version"))

	r.Config = readConfig()
	r.RecentLogs = readRecentLogs(100)

	return r
}

// FormatMarkdownWithDesc formats the report with a user-provided description.
func (r *Report) FormatMarkdownWithDesc(description string) string {
	return r.formatMarkdown(description)
}

// FormatMarkdown formats the report as a GitHub issue body.
func (r *Report) FormatMarkdown() string {
	return r.formatMarkdown("")
}

func (r *Report) formatMarkdown(description string) string {
	home, _ := os.UserHomeDir()
	sanitize := func(s string) string {
		if home != "" {
			return strings.ReplaceAll(s, home, "~")
		}
		return s
	}

	var b strings.Builder

	b.WriteString("## Bug Report\n\n")
	b.WriteString("### Description\n")
	if description != "" {
		b.WriteString(sanitize(description) + "\n\n")
	} else {
		b.WriteString("<!-- Please describe what happened -->\n\n")
	}

	// Recent Errors.
	if len(r.RecentErrors) > 0 {
		b.WriteString("### Recent Errors\n")
		b.WriteString("| Time | Error |\n|------|-------|\n")
		for _, e := range r.RecentErrors {
			b.WriteString("| " + sanitize(e) + " |\n")
		}
		b.WriteString("\n")
	}

	// Steps to Reproduce.
	if len(r.RecentActions) > 0 {
		b.WriteString("### Steps to Reproduce (last 20 actions)\n")
		b.WriteString("| Time | Action | Detail | Result |\n|------|--------|--------|--------|\n")
		for _, a := range r.RecentActions {
			b.WriteString("| " + sanitize(a) + " |\n")
		}
		b.WriteString("\n")
	}

	// Diagnostics.
	b.WriteString("### Diagnostics\n")
	fmt.Fprintf(&b, "- **Version**: %s\n", r.Version)
	if r.MacOSVersion != "" {
		fmt.Fprintf(&b, "- **macOS**: %s (%s)\n", r.MacOSVersion, r.Arch)
	} else {
		fmt.Fprintf(&b, "- **OS**: %s/%s\n", r.OS, r.Arch)
	}
	if r.TmuxVersion != "" {
		fmt.Fprintf(&b, "- **tmux**: %s\n", r.TmuxVersion)
	}
	if r.ClaudeVersion != "" {
		fmt.Fprintf(&b, "- **Claude CLI**: %s\n", sanitize(r.ClaudeVersion))
	}
	if r.GhVersion != "" {
		fmt.Fprintf(&b, "- **gh CLI**: %s\n", r.GhVersion)
	}
	fmt.Fprintf(&b, "- **Sessions**: %d\n", r.SessionCount)
	b.WriteString("\n")

	// Debug logs.
	if r.RecentLogs != "" {
		b.WriteString("<details><summary>Debug Log (last 100 lines)</summary>\n\n```\n")
		b.WriteString(sanitize(r.RecentLogs))
		b.WriteString("\n```\n</details>\n\n")
	}

	// Config.
	if r.Config != "" {
		b.WriteString("<details><summary>Config</summary>\n\n```json\n")
		b.WriteString(sanitize(r.Config))
		b.WriteString("\n```\n</details>\n")
	}

	return b.String()
}

func runCmd(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func readConfig() string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "brizz-code", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func readRecentLogs(n int) string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".config", "brizz-code", "debug.log")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
