package session

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yuvalhayke/brizz-code/internal/debuglog"
	"github.com/yuvalhayke/brizz-code/internal/tmux"
)

// Status represents the current state of a session.
type Status string

const (
	StatusRunning  Status = "running"
	StatusWaiting  Status = "waiting"
	StatusFinished Status = "finished"
	StatusIdle     Status = "idle"
	StatusError    Status = "error"
	StatusStarting Status = "starting"
)

// Session represents a managed Claude Code session.
type Session struct {
	ID              string
	Title           string
	ProjectPath     string
	Status          Status
	TmuxSessionName string
	CreatedAt       time.Time
	LastAccessedAt  time.Time
	Acknowledged    bool

	hookStatus    string
	hookUpdatedAt time.Time

	tmuxSession *tmux.Session
	mu          sync.RWMutex
}

// NewSession creates a new session for the given project path.
func NewSession(title, projectPath string) *Session {
	id := generateID()
	ts := tmux.NewSession(title, projectPath)

	return &Session{
		ID:              id,
		Title:           title,
		ProjectPath:     projectPath,
		Status:          StatusIdle,
		TmuxSessionName: ts.Name,
		CreatedAt:       time.Now(),
		tmuxSession:     ts,
	}
}

// Start launches the Claude Code session in tmux.
func (s *Session) Start() error {
	s.mu.Lock()
	s.Status = StatusStarting
	s.mu.Unlock()

	cmd := fmt.Sprintf("BRIZZCODE_INSTANCE_ID=%s claude", s.ID)
	if err := s.tmuxSession.Start(cmd); err != nil {
		s.mu.Lock()
		s.Status = StatusError
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	s.Status = StatusRunning
	s.mu.Unlock()
	return nil
}

// Kill terminates the tmux session.
func (s *Session) Kill() error {
	err := s.tmuxSession.Kill()
	s.mu.Lock()
	s.Status = StatusError
	s.mu.Unlock()
	return err
}

// GetStatus returns the current status (thread-safe).
func (s *Session) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Status
}

// SetStatus sets the status (thread-safe). Clears Acknowledged on Running.
func (s *Session) SetStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	if status == StatusRunning {
		s.Acknowledged = false
	}
}

// IsAlive checks if the tmux session exists.
func (s *Session) IsAlive() bool {
	return s.tmuxSession.Exists()
}

// GetTmuxSession returns the underlying tmux session handle.
func (s *Session) GetTmuxSession() *tmux.Session {
	return s.tmuxSession
}

// MarkAccessed updates the last accessed timestamp.
func (s *Session) MarkAccessed() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastAccessedAt = time.Now()
}

// Acknowledge marks the session as seen by the user.
func (s *Session) Acknowledge() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Acknowledged = true
	if s.Status == StatusFinished {
		s.Status = StatusIdle
	}
}

// HookStatus holds decoded status from a hook status file.
// Defined here to avoid import cycle with hooks package.
type HookStatus struct {
	Status    string
	UpdatedAt time.Time
}

// UpdateHookStatus updates the session's hook-based status.
func (s *Session) UpdateHookStatus(hs *HookStatus) {
	if hs == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hookStatus = hs.Status
	s.hookUpdatedAt = hs.UpdatedAt
}

// Restart kills and recreates the tmux session with the same config.
func (s *Session) Restart() error {
	// Kill old tmux session if it still exists.
	if s.tmuxSession.Exists() {
		_ = s.tmuxSession.Kill()
	}

	// Recreate tmux session with same config.
	s.tmuxSession = tmux.NewSession(s.Title, s.ProjectPath)
	s.mu.Lock()
	s.TmuxSessionName = s.tmuxSession.Name
	s.Status = StatusStarting
	s.mu.Unlock()

	cmd := fmt.Sprintf("BRIZZCODE_INSTANCE_ID=%s claude", s.ID)
	if err := s.tmuxSession.Start(cmd); err != nil {
		s.mu.Lock()
		s.Status = StatusError
		s.mu.Unlock()
		return err
	}

	s.mu.Lock()
	s.Status = StatusRunning
	s.mu.Unlock()
	return nil
}

// RespawnClaude restarts the claude process in an existing tmux session.
func (s *Session) RespawnClaude() error {
	s.mu.Lock()
	s.Status = StatusStarting
	s.mu.Unlock()

	cmd := fmt.Sprintf("BRIZZCODE_INSTANCE_ID=%s claude", s.ID)
	if err := s.tmuxSession.RespawnPane(cmd); err != nil {
		s.mu.Lock()
		s.Status = StatusError
		s.mu.Unlock()
		return err
	}

	// Reconfigure status bar after respawn.
	s.tmuxSession.ConfigureStatusBar()

	s.mu.Lock()
	s.Status = StatusRunning
	s.mu.Unlock()
	return nil
}

// UpdateStatus detects the session status from pane content.
func (s *Session) UpdateStatus() {
	log := debuglog.Logger.With("session", s.ID, "title", s.Title)
	oldStatus := s.GetStatus()

	if !s.IsAlive() {
		s.SetStatus(StatusError)
		log.Debug("status: not alive", "old", oldStatus, "new", StatusError)
		return
	}

	// Check if the pane's process has died (tmux alive but process crashed).
	if s.tmuxSession.IsPaneDead() {
		s.SetStatus(StatusError)
		log.Debug("status: pane dead", "old", oldStatus, "new", StatusError)
		return
	}

	// Hook fast path: hooks are authoritative as long as the session is alive.
	// No time-based expiry — IsAlive/IsPaneDead above handle stale scenarios.
	s.mu.RLock()
	hookStatus := s.hookStatus
	hookAge := time.Since(s.hookUpdatedAt)
	hasHook := hookStatus != "" && !s.hookUpdatedAt.IsZero()
	s.mu.RUnlock()

	if hasHook {
		s.mu.Lock()
		defer s.mu.Unlock()
		switch hookStatus {
		case "running":
			s.Status = StatusRunning
			s.Acknowledged = false
		case "waiting":
			s.Status = StatusWaiting
		case "finished":
			if s.Acknowledged {
				s.Status = StatusIdle
			} else {
				s.Status = StatusFinished
			}
		case "dead":
			s.Status = StatusError
		}
		if s.Status != oldStatus {
			log.Info("status changed (hook)", "old", oldStatus, "new", s.Status, "hookStatus", hookStatus, "hookAge", hookAge.Round(time.Millisecond))
		}
		return
	}

	// Pane capture fallback (no hook data available).
	log.Debug("no hook data, falling back to pane capture")

	content, err := s.tmuxSession.CapturePane()
	if err != nil {
		log.Warn("pane capture failed", "err", err)
		return // Keep previous status on capture failure.
	}

	// Strip ANSI escape codes for reliable pattern matching.
	content = stripANSI(content)

	status := detectStatus(content, log)

	s.mu.Lock()
	defer s.mu.Unlock()

	switch status {
	case StatusRunning:
		s.Status = StatusRunning
		s.Acknowledged = false
	case StatusWaiting:
		s.Status = StatusWaiting
	case StatusFinished:
		if s.Acknowledged {
			s.Status = StatusIdle
		} else {
			s.Status = StatusFinished
		}
	default:
		// No pattern matched — keep previous status instead of assuming running.
		log.Debug("no pattern matched, keeping previous status", "status", s.Status)
	}

	if s.Status != oldStatus {
		log.Info("status changed (pane)", "old", oldStatus, "new", s.Status, "detected", status)
	}
}

// ToRow converts to a storage row.
func (s *Session) ToRow() *SessionRow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return &SessionRow{
		ID:           s.ID,
		Title:        s.Title,
		ProjectPath:  s.ProjectPath,
		Status:       string(s.Status),
		TmuxSession:  s.TmuxSessionName,
		CreatedAt:    s.CreatedAt,
		LastAccessed: s.LastAccessedAt,
		Acknowledged: s.Acknowledged,
	}
}

// FromRow reconstructs a Session from a storage row, reconnecting to tmux.
func FromRow(row *SessionRow) *Session {
	ts := tmux.ReconnectSession(row.TmuxSession, row.Title, row.ProjectPath)
	status := Status(row.Status)
	// Don't check ts.Exists() here — let background worker detect dead sessions.

	return &Session{
		ID:              row.ID,
		Title:           row.Title,
		ProjectPath:     row.ProjectPath,
		Status:          status,
		TmuxSessionName: row.TmuxSession,
		CreatedAt:       row.CreatedAt,
		LastAccessedAt:  row.LastAccessed,
		Acknowledged:    row.Acknowledged,
		tmuxSession:     ts,
	}
}

// --- Status detection ---

var (
	busyPatterns = []string{
		"ctrl+c to interrupt",
		"esc to interrupt",
	}
	spinnerChars = []string{
		"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏",
		"✳", "✽", "✶", "✢",
	}
	approvalPatterns = []string{
		"Yes, allow once",
		"No, and tell Claude",
		"Continue?",
		"(Y/n)",
		"(y/N)",
		"Do you trust the files",
		"Allow this MCP server",
	}
	// Patterns in recent lines that indicate Claude is idle at the prompt.
	idlePatterns = []string{
		"⏵⏵", // Claude Code permission mode bar (appears below the prompt)
	}
)

func detectStatus(content string, log *slog.Logger) Status {
	if content == "" {
		log.Debug("detectStatus: empty content")
		return ""
	}

	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")

	// Get last non-empty lines for analysis.
	var recentLines []string
	for i := len(lines) - 1; i >= 0 && len(recentLines) < 25; i-- {
		line := strings.TrimRight(lines[i], " \t")
		if line != "" {
			recentLines = append(recentLines, line)
		}
	}
	recentContent := strings.Join(recentLines, "\n")
	lowerContent := strings.ToLower(recentContent)

	// Check busy indicators first (highest priority).
	for _, pattern := range busyPatterns {
		if strings.Contains(lowerContent, pattern) {
			log.Debug("detectStatus: matched busy pattern", "pattern", pattern)
			return StatusRunning
		}
	}

	// Check spinner chars in recent lines.
	for _, line := range recentLines {
		for _, sc := range spinnerChars {
			if strings.Contains(line, sc) {
				log.Debug("detectStatus: matched spinner char", "char", sc, "line", line)
				return StatusRunning
			}
		}
	}

	// Check approval/permission prompts.
	for _, pattern := range approvalPatterns {
		if strings.Contains(recentContent, pattern) {
			log.Debug("detectStatus: matched approval pattern", "pattern", pattern)
			return StatusWaiting
		}
	}

	// Check for prompt indicator (Claude is idle, waiting for user input).
	if len(recentLines) > 0 {
		lastLine := strings.TrimSpace(recentLines[0]) // recentLines is reversed.
		if lastLine == ">" || lastLine == "❯" || strings.HasPrefix(lastLine, "> ") || strings.HasPrefix(lastLine, "❯ ") {
			log.Debug("detectStatus: matched prompt", "lastLine", lastLine)
			return StatusFinished
		}

		// Check idle patterns anywhere in recent lines (e.g. permission mode bar).
		for _, pattern := range idlePatterns {
			if strings.Contains(recentContent, pattern) {
				log.Debug("detectStatus: matched idle pattern", "pattern", pattern)
				return StatusFinished
			}
		}

		log.Debug("detectStatus: no pattern matched",
			"lastLine", lastLine,
			"lastLineHex", fmt.Sprintf("%x", lastLine),
			"recentLineCount", len(recentLines),
		)
	} else {
		log.Debug("detectStatus: no recent lines found")
	}

	return "" // No match — caller keeps previous status.
}

// stripANSI removes ANSI escape sequences from content.
// Uses O(n) single-pass algorithm to avoid regex backtracking issues.
func stripANSI(content string) string {
	// Fast path: no escape chars.
	if !strings.ContainsRune(content, '\x1b') && !strings.ContainsRune(content, '\x9B') {
		return content
	}

	var b strings.Builder
	b.Grow(len(content))

	i := 0
	for i < len(content) {
		if content[i] == '\x1b' {
			i++
			if i < len(content) && content[i] == '[' {
				// CSI sequence: skip until final byte (0x40-0x7E).
				i++
				for i < len(content) && content[i] >= 0x20 && content[i] <= 0x3F {
					i++ // parameter bytes
				}
				if i < len(content) && content[i] >= 0x40 && content[i] <= 0x7E {
					i++ // final byte
				}
			} else if i < len(content) && content[i] == ']' {
				// OSC sequence: skip until ST (ESC \ or BEL).
				i++
				for i < len(content) {
					if content[i] == '\x07' {
						i++
						break
					}
					if content[i] == '\x1b' && i+1 < len(content) && content[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
			} else {
				// Other escape: skip one byte after ESC.
				if i < len(content) {
					i++
				}
			}
		} else if content[i] == '\x9B' {
			// C1 CSI: skip until final byte.
			i++
			for i < len(content) && content[i] >= 0x20 && content[i] <= 0x3F {
				i++
			}
			if i < len(content) && content[i] >= 0x40 && content[i] <= 0x7E {
				i++
			}
		} else {
			b.WriteByte(content[i])
			i++
		}
	}

	return b.String()
}

// --- Repo grouping ---

var (
	repoRootCache   = make(map[string]string)
	repoRootCacheMu sync.RWMutex
)

// GetRepoRoot returns the git repo root for a path, or the path itself if not a git repo.
func GetRepoRoot(projectPath string) string {
	repoRootCacheMu.RLock()
	if root, ok := repoRootCache[projectPath]; ok {
		repoRootCacheMu.RUnlock()
		return root
	}
	repoRootCacheMu.RUnlock()

	cmd := exec.Command("git", "-C", projectPath, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	root := projectPath
	if err == nil {
		root = strings.TrimSpace(string(output))
	}

	repoRootCacheMu.Lock()
	repoRootCache[projectPath] = root
	repoRootCacheMu.Unlock()

	return root
}

// GroupByRepo groups sessions by their git repo root.
func GroupByRepo(sessions []*Session) map[string][]*Session {
	groups := make(map[string][]*Session)
	for _, s := range sessions {
		root := GetRepoRoot(s.ProjectPath)
		groups[root] = append(groups[root], s)
	}
	return groups
}

func generateID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x-%d", b, time.Now().Unix())
}

// TitleFromPath generates a session title from a directory path.
func TitleFromPath(path string) string {
	return filepath.Base(path)
}
