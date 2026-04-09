package ui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/brizzai/brizz-code/internal/debuglog"
	"github.com/brizzai/brizz-code/internal/hooks"
	"github.com/brizzai/brizz-code/internal/session"
)

type statusSnapshotMsg struct {
	path string
	err  error
}

func captureStatusSnapshot(s *session.Session, sessionID string) statusSnapshotMsg {
	ts := s.GetTmuxSession()
	if ts == nil {
		return statusSnapshotMsg{err: fmt.Errorf("no tmux session")}
	}

	// 1. Fresh pane capture with ANSI.
	rawPane, err := ts.CapturePaneFresh()
	if err != nil {
		return statusSnapshotMsg{err: fmt.Errorf("pane capture: %w", err)}
	}

	// 2. Session state snapshot.
	snap := s.SnapshotData(rawPane)

	// 3. Read hook file.
	hookFilePath := filepath.Join(hooks.GetHooksDir(), sessionID+".json")
	hookFileContent, _ := os.ReadFile(hookFilePath)
	var hookFileInfo os.FileInfo
	hookFileInfo, _ = os.Stat(hookFilePath)

	// 4. Filtered debug log tail.
	debugTail := readFilteredDebugLog(sessionID, 100)

	// 5. Create output directory.
	now := time.Now()
	safeTitle := sanitizeForPath(snap.Title)
	dirName := fmt.Sprintf("%s_%s", now.Format("2006-01-02T15-04-05"), safeTitle)
	home, _ := os.UserHomeDir()
	snapshotDir := filepath.Join(home, ".config", "brizz-code", "snapshots", dirName)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return statusSnapshotMsg{err: fmt.Errorf("mkdir: %w", err)}
	}

	// 6. Write files.
	_ = os.WriteFile(filepath.Join(snapshotDir, "pane_raw.txt"), []byte(rawPane), 0644)
	_ = os.WriteFile(filepath.Join(snapshotDir, "pane_clean.txt"), []byte(session.StripANSI(rawPane)), 0644)
	_ = os.WriteFile(filepath.Join(snapshotDir, "debug_tail.txt"), []byte(debugTail), 0644)

	meta := buildSnapshotJSON(snap, hookFileContent, hookFileInfo, now)
	jsonData, _ := json.MarshalIndent(meta, "", "  ")
	_ = os.WriteFile(filepath.Join(snapshotDir, "snapshot.json"), jsonData, 0644)

	debuglog.Logger.Info("status snapshot captured", "dir", snapshotDir, "session", sessionID)

	return statusSnapshotMsg{path: snapshotDir}
}

func buildSnapshotJSON(snap session.StatusSnapshot, hookFileRaw []byte, hookFileInfo os.FileInfo, now time.Time) map[string]any {
	m := map[string]any{
		"captured_at": now.Format(time.RFC3339Nano),
		"session": map[string]any{
			"id":               snap.ID,
			"title":            snap.Title,
			"project_path":     snap.ProjectPath,
			"tmux_session":     snap.TmuxSessionName,
			"claude_session_id": snap.ClaudeSessionID,
			"status":           string(snap.Status),
			"acknowledged":     snap.Acknowledged,
		},
	}

	hookMap := map[string]any{
		"status":       snap.HookStatus,
		"updated_at":   snap.HookUpdatedAt.Format(time.RFC3339),
		"age":          fmtSnapshotDuration(now.Sub(snap.HookUpdatedAt)),
		"overridden_at": snap.HookOverriddenAt.Format(time.RFC3339),
	}
	if len(hookFileRaw) > 0 {
		var parsed any
		if json.Unmarshal(hookFileRaw, &parsed) == nil {
			hookMap["file_contents"] = parsed
		}
	}
	if hookFileInfo != nil {
		hookMap["file_mod_time"] = hookFileInfo.ModTime().Format(time.RFC3339)
	}
	m["hook"] = hookMap

	m["content"] = map[string]any{
		"hash":            snap.LastContentHash,
		"last_change_at":  snap.LastContentChangeAt.Format(time.RFC3339),
		"last_change_ago": fmtSnapshotDuration(now.Sub(snap.LastContentChangeAt)),
	}

	paneDetected := string(snap.DetectedPaneStatus)
	tuiShows := string(snap.Status)
	m["detection"] = map[string]any{
		"pane_detected": paneDetected,
		"tui_shows":     tuiShows,
		"mismatch":      paneDetected != "" && paneDetected != tuiShows,
	}

	return m
}

func fmtSnapshotDuration(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	return d.Round(time.Millisecond).String()
}

func readFilteredDebugLog(sessionID string, maxLines int) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	logPath := filepath.Join(home, ".config", "brizz-code", "debug.log")
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, sessionID) {
			lines = append(lines, line)
			if len(lines) > maxLines*2 {
				lines = lines[len(lines)-maxLines:]
			}
		}
	}

	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	return strings.Join(lines, "\n")
}

var pathSanitizer = regexp.MustCompile(`[^a-zA-Z0-9-]`)

func sanitizeForPath(title string) string {
	s := strings.ReplaceAll(title, " ", "-")
	s = pathSanitizer.ReplaceAllString(s, "")
	if len(s) > 30 {
		s = s[:30]
	}
	if s == "" {
		s = "session"
	}
	return strings.ToLower(s)
}
