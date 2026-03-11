package session

import (
	"crypto/rand"
	"crypto/sha256"
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
	ClaudeSessionID string
	WorkspaceName   string
	ManuallyRenamed bool
	FirstPrompt     string
	TitleGenerated  bool
	PromptCount     int

	hookStatus    string
	hookUpdatedAt time.Time

	lastContentHash     string
	lastContentChangeAt time.Time

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

// buildClaudeCmd returns the claude command with optional --resume flag.
func (s *Session) buildClaudeCmd() string {
	cmd := "claude"
	if s.ClaudeSessionID != "" {
		cmd += fmt.Sprintf(" --resume %s", s.ClaudeSessionID)
	}
	return cmd
}

// sessionEnv returns the env vars to set on the tmux session for this brizz-code session.
func (s *Session) sessionEnv() []string {
	return []string{
		fmt.Sprintf("BRIZZCODE_INSTANCE_ID=%s", s.ID),
		"ZSH_DOTENV_PROMPT=false", // Auto-source .env without prompting (oh-my-zsh dotenv plugin).
	}
}

// Start launches the Claude Code session in tmux.
func (s *Session) Start() error {
	s.mu.Lock()
	s.Status = StatusStarting
	s.mu.Unlock()

	cmd := s.buildClaudeCmd()
	if err := s.tmuxSession.Start(cmd, s.sessionEnv()...); err != nil {
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

// SetStatus sets the status (thread-safe). Clears Acknowledged on active states.
func (s *Session) SetStatus(status Status) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = status
	if status == StatusRunning || status == StatusWaiting {
		s.Acknowledged = false
	}
}

// GetHookStatus returns the raw hook status string (thread-safe).
func (s *Session) GetHookStatus() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hookStatus
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
	Status      string
	SessionID   string // Claude conversation session ID
	UpdatedAt   time.Time
	UserPrompt  string
	PromptCount int
}

// UpdateHookStatus updates the session's hook-based status.
func (s *Session) UpdateHookStatus(hs *HookStatus) {
	if hs == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Reset content hash tracking when hook status changes (fresh tracking for new state).
	if s.hookStatus != hs.Status {
		s.lastContentHash = ""
		s.lastContentChangeAt = time.Time{}
	}
	s.hookStatus = hs.Status
	s.hookUpdatedAt = hs.UpdatedAt
	if hs.SessionID != "" {
		s.ClaudeSessionID = hs.SessionID
	}
	// Track user prompts for auto-naming (always update to latest).
	if hs.PromptCount > s.PromptCount {
		s.PromptCount = hs.PromptCount
		if hs.UserPrompt != "" {
			s.FirstPrompt = hs.UserPrompt
		}
	} else if hs.UserPrompt != "" && s.FirstPrompt == "" {
		s.FirstPrompt = hs.UserPrompt
	}
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

	cmd := s.buildClaudeCmd()
	if err := s.tmuxSession.Start(cmd, s.sessionEnv()...); err != nil {
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

	cmd := s.buildClaudeCmd()
	if err := s.tmuxSession.RespawnPane(cmd, s.sessionEnv()...); err != nil {
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
		// Capture pane once for content change detection and pane-based overrides.
		var paneContent string
		var paneStatus Status
		if content, err := s.tmuxSession.CapturePane(); err == nil {
			paneContent = stripANSI(content)
			paneStatus = detectStatus(paneContent, log)
		}

		s.mu.Lock()
		defer s.mu.Unlock()
		switch hookStatus {
		case "running":
			s.Status = StatusRunning
			s.Acknowledged = false
			// Only override to waiting immediately (permission prompts are unambiguous).
			// Do NOT override to finished here — the old ❯ prompt from the previous turn
			// is always visible in scrollback and causes false "finished" flashes between
			// spinner frames. Let content stability (10s) handle running→finished.
			if paneStatus == StatusWaiting {
				s.Status = paneStatus
				log.Info("hook says running but pane shows waiting, overriding")
			}
			if paneContent != "" {
				// Content change detection: if content is stable >10s, Claude likely stopped.
				hash := hashContent(normalizeForHash(paneContent))
				if hash != s.lastContentHash {
					s.lastContentHash = hash
					s.lastContentChangeAt = time.Now()
				} else if !s.lastContentChangeAt.IsZero() && time.Since(s.lastContentChangeAt) > 10*time.Second {
					// Content stable >10s while hook says running — Claude likely stopped
					// (e.g. user pressed Escape, no Stop hook fires).
					s.Status = StatusFinished
					log.Info("content stable >10s, hook says running, assuming finished",
						"stableSince", s.lastContentChangeAt.Format(time.TimeOnly))
				}
			}
		case "waiting":
			// Hook says waiting — trust hooks fully, never override from pane.
			//
			// Why no pane override at all:
			// - waiting→finished: ❯ prompt match false-positives on Claude's menu
			//   selector (e.g. "❯ 1. Yes, allow once")
			// - waiting→running: stale "Running…" / spinner text from subagent output
			//   above the permission prompt triggers whimsical/spinner patterns
			// - If user approves, UserPromptSubmit hook fires within milliseconds
			// - If user denies/escapes, idle_prompt hook at ~60s handles it
			// - Content change detection below handles the gap if hooks are delayed
			s.Status = StatusWaiting
			s.Acknowledged = false
			if paneContent != "" {
				// Content change detection: if content changed since waiting started, user acted.
				hash := hashContent(normalizeForHash(paneContent))
				if s.lastContentHash == "" {
					// First tick in waiting state — save baseline hash.
					s.lastContentHash = hash
					s.lastContentChangeAt = time.Now()
				} else if hash != s.lastContentHash {
					// Content changed — user acted on the prompt.
					// Transition to running (approval is the most common action).
					// Hooks will correct to the right status within milliseconds.
					s.lastContentHash = hash
					s.lastContentChangeAt = time.Now()
					s.Status = StatusRunning
					log.Info("content changed while waiting, assuming running")
				}
			}
		case "finished":
			s.lastContentHash = ""
			s.lastContentChangeAt = time.Time{}
			if paneStatus == StatusRunning {
				// Hook says finished (e.g. SessionStart after auto-resume) but pane
				// shows an active spinner — Claude is actually working.
				s.Status = StatusRunning
				log.Info("hook says finished but pane shows running, overriding")
			} else if s.Acknowledged {
				s.Status = StatusIdle
			} else {
				s.Status = StatusFinished
			}
		case "dead":
			s.lastContentHash = ""
			s.lastContentChangeAt = time.Time{}
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
		s.Acknowledged = false
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
		ID:              s.ID,
		Title:           s.Title,
		ProjectPath:     s.ProjectPath,
		Status:          string(s.Status),
		TmuxSession:     s.TmuxSessionName,
		CreatedAt:       s.CreatedAt,
		LastAccessed:    s.LastAccessedAt,
		Acknowledged:    s.Acknowledged,
		ClaudeSessionID: s.ClaudeSessionID,
		WorkspaceName:   s.WorkspaceName,
		ManuallyRenamed: s.ManuallyRenamed,
		FirstPrompt:     s.FirstPrompt,
		TitleGenerated:  s.TitleGenerated,
		PromptCount:     s.PromptCount,
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
		ClaudeSessionID: row.ClaudeSessionID,
		WorkspaceName:   row.WorkspaceName,
		ManuallyRenamed: row.ManuallyRenamed,
		FirstPrompt:     row.FirstPrompt,
		TitleGenerated:  row.TitleGenerated,
		PromptCount:     row.PromptCount,
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
		"⏵⏵",            // Claude Code permission mode bar (appears below the prompt)
		"esc to cancel",  // Claude Code text input prompt (commit message, etc.)
		"tab to amend",   // Claude Code text input prompt
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
	for i := len(lines) - 1; i >= 0 && len(recentLines) < 50; i-- {
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

	// Check whimsical activity pattern (Claude 2.1.25+: "Clauding… (53s · ↓ 749 tokens)").
	if strings.Contains(lowerContent, "…") && strings.Contains(lowerContent, "tokens") {
		log.Debug("detectStatus: matched whimsical activity pattern")
		return StatusRunning
	}

	// Check approval/permission prompts.
	for _, pattern := range approvalPatterns {
		if strings.Contains(recentContent, pattern) {
			log.Debug("detectStatus: matched approval pattern", "pattern", pattern)
			return StatusWaiting
		}
	}

	// Check for prompt indicator (Claude is idle, waiting for user input).
	// Scan last few lines since Claude Code renders a separator + status bar below the prompt.
	if len(recentLines) > 0 {
		scanLimit := 5
		if scanLimit > len(recentLines) {
			scanLimit = len(recentLines)
		}
		for i := 0; i < scanLimit; i++ {
			line := strings.TrimSpace(recentLines[i])
			if line == ">" || line == "❯" || strings.HasPrefix(line, "> ") || strings.HasPrefix(line, "❯ ") {
				log.Debug("detectStatus: matched prompt", "line", line, "linesFromBottom", i)
				return StatusFinished
			}
		}

		// Check idle patterns anywhere in recent lines (e.g. permission mode bar).
		for _, pattern := range idlePatterns {
			if strings.Contains(recentContent, pattern) {
				log.Debug("detectStatus: matched idle pattern", "pattern", pattern)
				return StatusFinished
			}
		}

		lastLine := strings.TrimSpace(recentLines[0])
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

// normalizeForHash normalizes pane content for stable hashing.
// Strips ANSI, spinner chars, trailing whitespace, and collapses blank lines.
func normalizeForHash(content string) string {
	content = stripANSI(content)
	// Strip spinner characters.
	for _, sc := range spinnerChars {
		content = strings.ReplaceAll(content, sc, "")
	}
	// Trim trailing whitespace per line and collapse consecutive blank lines.
	lines := strings.Split(content, "\n")
	var result []string
	prevBlank := false
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		blank := line == ""
		if blank && prevBlank {
			continue
		}
		result = append(result, line)
		prevBlank = blank
	}
	return strings.Join(result, "\n")
}

// hashContent returns a truncated SHA256 hash (16 hex chars) of the content.
func hashContent(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8])
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
