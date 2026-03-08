package ui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/session"
	"github.com/yuvalhayke/brizz-code/internal/tmux"
)

const (
	tickInterval           = 2 * time.Second
	previewCacheTTL        = 500 * time.Millisecond
	layoutBreakpointSingle = 50
	layoutBreakpointDual   = 80
	helpBarHeight          = 2 // border line + shortcuts
	statusRoundRobin       = 5 // sessions per tick
)

// Message types.
type (
	tickMsg          time.Time
	statusUpdateMsg  struct{ attachedSessionID string }
	sessionDeleteMsg struct {
		id  string
		err error
	}
	sessionRestartMsg struct {
		id  string
		err error
	}
	previewMsg struct {
		sessionID string
		content   string
	}
	loadSessionsMsg struct {
		sessions []*session.Session
		err      error
	}
)

// Home is the main Bubble Tea model.
type Home struct {
	width  int
	height int

	sessions    []*session.Session
	sessionByID map[string]*session.Session
	storage     *session.StateDB
	flatItems   []SidebarItem

	cursor     int
	viewOffset int

	isAttaching atomic.Bool
	err         error
	errTime     time.Time

	newDialog     *NewSessionDialog
	confirmDialog *ConfirmDialog
	helpOverlay   *HelpOverlay

	previewCache     map[string]string
	previewCacheTime map[string]time.Time
	statusRRIndex    int // round-robin index for status updates
}

// NewHome creates the main TUI model.
func NewHome(storage *session.StateDB) *Home {
	return &Home{
		storage:          storage,
		sessionByID:      make(map[string]*session.Session),
		newDialog:        NewNewSessionDialog(),
		confirmDialog:    NewConfirmDialog(),
		helpOverlay:      NewHelpOverlay(),
		previewCache:     make(map[string]string),
		previewCacheTime: make(map[string]time.Time),
	}
}

// Init implements tea.Model.
func (h *Home) Init() tea.Cmd {
	return tea.Batch(
		h.loadSessions,
		h.tick(),
	)
}

// Update implements tea.Model.
func (h *Home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.width = msg.Width
		h.height = msg.Height
		h.newDialog.SetSize(msg.Width, msg.Height)
		h.confirmDialog.SetSize(msg.Width, msg.Height)
		h.helpOverlay.SetSize(msg.Width, msg.Height)
		h.syncViewport()
		return h, nil

	case tea.KeyMsg:
		return h.handleKey(msg)

	case tickMsg:
		return h.handleTick()

	case statusUpdateMsg:
		// Returned after detaching from session.
		h.isAttaching.Store(false)
		tmux.RefreshSessionCache()
		h.updateAllStatuses()
		h.rebuildFlatItems()
		return h, nil

	case sessionCreateMsg:
		return h.handleSessionCreate(msg)

	case sessionDeleteMsg:
		if msg.err != nil {
			h.setError(msg.err)
		} else {
			h.deleteSession(msg.id)
		}
		return h, nil

	case sessionRestartMsg:
		if msg.err != nil {
			h.setError(fmt.Errorf("restart failed: %w", msg.err))
		}
		// Update storage with new status and tmux session name.
		if s, ok := h.sessionByID[msg.id]; ok {
			_ = h.storage.UpdateStatus(s.ID, string(s.GetStatus()))
			_ = h.storage.UpdateTmuxSession(s.ID, s.TmuxSessionName)
		}
		h.rebuildFlatItems()
		return h, nil

	case previewMsg:
		h.previewCache[msg.sessionID] = msg.content
		h.previewCacheTime[msg.sessionID] = time.Now()
		return h, nil

	case loadSessionsMsg:
		if msg.err != nil {
			h.setError(msg.err)
			return h, nil
		}
		h.sessions = msg.sessions
		h.rebuildSessionMap()
		h.rebuildFlatItems()
		if len(h.flatItems) > 0 && h.cursor == 0 {
			h.cursor = FirstSelectableItem(h.flatItems)
		}
		return h, nil
	}

	return h, nil
}

// View implements tea.Model.
func (h *Home) View() string {
	if h.isAttaching.Load() {
		return ""
	}
	if h.width == 0 {
		return "Loading..."
	}

	// Modals take priority.
	if h.helpOverlay.IsVisible() {
		return h.helpOverlay.View()
	}
	if h.newDialog.IsVisible() {
		return h.newDialog.View()
	}
	if h.confirmDialog.IsVisible() {
		return h.confirmDialog.View()
	}

	var b strings.Builder

	// Header.
	header := h.renderHeader()
	b.WriteString(header)
	b.WriteString("\n")

	// Content area.
	contentHeight := h.height - 2 - helpBarHeight // header + help bar
	if contentHeight < 1 {
		contentHeight = 1
	}

	switch h.layoutMode() {
	case "single":
		sidebar := RenderSidebar(h.flatItems, h.cursor, h.viewOffset, h.width, contentHeight)
		b.WriteString(sidebar)
	case "stacked":
		sidebarHeight := (contentHeight * 55) / 100
		if sidebarHeight < 3 {
			sidebarHeight = 3
		}
		previewHeight := contentHeight - sidebarHeight - 1 // 1 for separator
		sidebar := RenderSidebar(h.flatItems, h.cursor, h.viewOffset, h.width, sidebarHeight)
		b.WriteString(sidebar)
		b.WriteString("\n")
		b.WriteString(DimStyle.Render(strings.Repeat("─", h.width)))
		b.WriteString("\n")
		s, content := h.selectedPreview()
		preview := RenderPreview(s, content, h.width, previewHeight)
		b.WriteString(preview)
	default: // dual
		sidebarWidth := h.width * 35 / 100
		if sidebarWidth < 20 {
			sidebarWidth = 20
		}
		previewWidth := h.width - sidebarWidth - 3 // 3 for separator

		sidebar := RenderSidebar(h.flatItems, h.cursor, h.viewOffset, sidebarWidth, contentHeight)
		s, content := h.selectedPreview()
		preview := RenderPreview(s, content, previewWidth, contentHeight)

		// Side by side with separator.
		sidebarBox := lipgloss.NewStyle().Width(sidebarWidth).Height(contentHeight).Render(sidebar)
		sep := lipgloss.NewStyle().
			Foreground(ColorBorder).
			Width(1).
			Height(contentHeight).
			Render(strings.Repeat("│\n", contentHeight))
		previewBox := lipgloss.NewStyle().Width(previewWidth).Height(contentHeight).Render(preview)

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, sidebarBox, " "+sep+" ", previewBox))
	}

	// Pad to fill content area.
	lines := strings.Count(b.String(), "\n") + 1
	for lines < h.height-helpBarHeight {
		b.WriteString("\n")
		lines++
	}

	// Help bar.
	b.WriteString("\n")
	b.WriteString(h.renderHelpBar())

	// Error message (overwrites last line if present).
	if h.err != nil && time.Since(h.errTime) < 5*time.Second {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render(" " + h.err.Error()))
	}

	return b.String()
}

// --- Key handling ---

func (h *Home) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Route to active dialog/overlay.
	if h.helpOverlay.IsVisible() {
		overlay, cmd := h.helpOverlay.Update(msg)
		h.helpOverlay = overlay
		return h, cmd
	}
	if h.newDialog.IsVisible() {
		dialog, cmd := h.newDialog.Update(msg)
		h.newDialog = dialog
		return h, cmd
	}
	if h.confirmDialog.IsVisible() {
		dialog, cmd := h.confirmDialog.Update(msg)
		h.confirmDialog = dialog
		return h, cmd
	}

	switch msg.String() {
	case "j", "down":
		h.cursor = NextSelectableItem(h.flatItems, h.cursor, 1)
		h.syncViewport()
		return h, nil
	case "k", "up":
		h.cursor = NextSelectableItem(h.flatItems, h.cursor, -1)
		h.syncViewport()
		return h, nil
	case "enter":
		return h, h.attachSelected()
	case "a", "n":
		h.newDialog.Show()
		return h, nil
	case "d":
		return h, h.confirmDeleteSelected()
	case "r":
		return h, h.restartSelected()
	case "?":
		h.helpOverlay.Show()
		return h, nil
	case "q", "ctrl+c":
		return h, tea.Quit
	}

	return h, nil
}

// --- Session operations ---

func (h *Home) attachSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil || !s.IsAlive() {
		return nil
	}

	s.MarkAccessed()
	s.Acknowledge()
	_ = h.storage.SetAcknowledged(s.ID, true)
	_ = h.storage.UpdateLastAccessed(s.ID)

	h.isAttaching.Store(true)

	return tea.Exec(attachCmd{session: s.GetTmuxSession()}, func(err error) tea.Msg {
		// CRITICAL: Clear isAttaching before returning the message.
		// Prevents race where View() returns empty string after detach.
		h.isAttaching.Store(false)
		return statusUpdateMsg{attachedSessionID: s.ID}
	})
}

type attachCmd struct {
	session *tmux.Session
}

func (a attachCmd) Run() error {
	return a.session.Attach(context.Background())
}

func (a attachCmd) SetStdin(r io.Reader)  {}
func (a attachCmd) SetStdout(w io.Writer) {}
func (a attachCmd) SetStderr(w io.Writer) {}

func (h *Home) handleSessionCreate(msg sessionCreateMsg) (tea.Model, tea.Cmd) {
	s := session.NewSession(msg.title, msg.path)
	if err := s.Start(); err != nil {
		h.setError(fmt.Errorf("failed to start session: %w", err))
		return h, nil
	}

	h.sessions = append(h.sessions, s)
	h.rebuildSessionMap()
	h.rebuildFlatItems()

	// Save to storage.
	if err := h.storage.SaveSession(s.ToRow()); err != nil {
		h.setError(fmt.Errorf("failed to save session: %w", err))
	}

	// Auto-select the new session.
	for i, item := range h.flatItems {
		if !item.IsRepoHeader && item.Session != nil && item.Session.ID == s.ID {
			h.cursor = i
			h.syncViewport()
			break
		}
	}

	return h, nil
}

func (h *Home) confirmDeleteSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil {
		return nil
	}

	id := s.ID
	h.confirmDialog.Show(
		fmt.Sprintf("Delete session '%s'?", s.Title),
		func() tea.Msg {
			return sessionDeleteMsg{id: id}
		},
	)
	return nil
}

func (h *Home) restartSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil {
		return nil
	}

	id := s.ID
	return func() tea.Msg {
		var err error
		if s.IsAlive() && !s.GetTmuxSession().IsPaneDead() {
			// Tmux session alive, just respawn the pane.
			err = s.RespawnClaude()
		} else {
			// Tmux session dead or pane dead — full restart.
			err = s.Restart()
		}
		return sessionRestartMsg{id: id, err: err}
	}
}

func (h *Home) deleteSession(id string) {
	s, ok := h.sessionByID[id]
	if !ok {
		return
	}

	// Kill tmux session if alive.
	if s.IsAlive() {
		_ = s.Kill()
	}

	// Remove from storage.
	_ = h.storage.DeleteSession(id)

	// Remove from list.
	var remaining []*session.Session
	for _, sess := range h.sessions {
		if sess.ID != id {
			remaining = append(remaining, sess)
		}
	}
	h.sessions = remaining
	h.rebuildSessionMap()
	h.rebuildFlatItems()

	// Fix cursor.
	if h.cursor >= len(h.flatItems) {
		h.cursor = len(h.flatItems) - 1
	}
	if h.cursor < 0 {
		h.cursor = 0
	}
	if len(h.flatItems) > 0 && h.flatItems[h.cursor].IsRepoHeader {
		h.cursor = NextSelectableItem(h.flatItems, h.cursor, 1)
	}
}

// --- Tick / status ---

func (h *Home) tick() tea.Cmd {
	return tea.Tick(tickInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (h *Home) handleTick() (tea.Model, tea.Cmd) {
	tmux.RefreshSessionCache()

	// Round-robin status updates.
	if len(h.sessions) > 0 {
		count := statusRoundRobin
		if count > len(h.sessions) {
			count = len(h.sessions)
		}
		for i := 0; i < count; i++ {
			idx := (h.statusRRIndex + i) % len(h.sessions)
			s := h.sessions[idx]
			oldStatus := s.GetStatus()
			s.UpdateStatus()
			newStatus := s.GetStatus()
			// Persist status changes.
			if oldStatus != newStatus {
				_ = h.storage.UpdateStatus(s.ID, string(newStatus))
			}
		}
		h.statusRRIndex = (h.statusRRIndex + count) % len(h.sessions)
	}

	// Fetch preview for selected session.
	var previewCmd tea.Cmd
	if sel := h.selectedSession(); sel != nil && sel.IsAlive() {
		if _, ok := h.previewCacheTime[sel.ID]; !ok || time.Since(h.previewCacheTime[sel.ID]) > previewCacheTTL {
			previewCmd = h.fetchPreview(sel)
		}
	}

	h.rebuildFlatItems()

	cmds := []tea.Cmd{h.tick()}
	if previewCmd != nil {
		cmds = append(cmds, previewCmd)
	}
	return h, tea.Batch(cmds...)
}

func (h *Home) updateAllStatuses() {
	for _, s := range h.sessions {
		oldStatus := s.GetStatus()
		s.UpdateStatus()
		newStatus := s.GetStatus()
		if oldStatus != newStatus {
			_ = h.storage.UpdateStatus(s.ID, string(newStatus))
		}
	}
}

func (h *Home) fetchPreview(s *session.Session) tea.Cmd {
	id := s.ID
	ts := s.GetTmuxSession()
	return func() tea.Msg {
		content, _ := ts.CapturePane()
		return previewMsg{sessionID: id, content: content}
	}
}

// --- Rendering helpers ---

func (h *Home) renderHeader() string {
	statusCounts := make(map[session.Status]int)
	for _, s := range h.sessions {
		statusCounts[s.GetStatus()]++
	}

	title := TitleStyle.Render(" brizz-code ")

	// Build status indicators — only show non-zero.
	var indicators []string
	if n := statusCounts[session.StatusRunning] + statusCounts[session.StatusStarting]; n > 0 {
		indicators = append(indicators, StatusRunningStyle.Render(fmt.Sprintf("● %d running", n)))
	}
	if n := statusCounts[session.StatusWaiting]; n > 0 {
		indicators = append(indicators, StatusWaitingStyle.Render(fmt.Sprintf("◐ %d waiting", n)))
	}
	if n := statusCounts[session.StatusFinished]; n > 0 {
		indicators = append(indicators, StatusFinishedStyle.Render(fmt.Sprintf("● %d finished", n)))
	}
	if n := statusCounts[session.StatusIdle]; n > 0 {
		indicators = append(indicators, StatusIdleStyle.Render(fmt.Sprintf("○ %d idle", n)))
	}
	if n := statusCounts[session.StatusError]; n > 0 {
		indicators = append(indicators, StatusErrorStyle.Render(fmt.Sprintf("✕ %d error", n)))
	}

	sep := lipgloss.NewStyle().Foreground(ColorBorder).Render(" • ")
	right := strings.Join(indicators, sep)

	headerLeft := title
	gap := h.width - lipgloss.Width(headerLeft) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	content := headerLeft + strings.Repeat(" ", gap) + right
	return HeaderBarStyle.Width(h.width).Render(content)
}

func (h *Home) renderHelpBar() string {
	// Border line.
	border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))

	// Key-description pairs.
	type keyDesc struct{ key, desc string }
	contextKeys := []keyDesc{
		{"⏎", "Attach"},
		{"a", "New"},
		{"d", "Delete"},
		{"r", "Restart"},
	}
	globalKeys := []keyDesc{
		{"j/k", "Nav"},
		{"?", "Help"},
		{"q", "Quit"},
	}

	renderPair := func(kd keyDesc) string {
		return HelpKeyStyle.Render(kd.key) + " " + HelpDescStyle.Render(kd.desc)
	}

	var parts []string
	for _, kd := range contextKeys {
		parts = append(parts, renderPair(kd))
	}
	sep := HelpSepStyle.Render(" │ ")
	left := strings.Join(parts, "  ")

	var gparts []string
	for _, kd := range globalKeys {
		gparts = append(gparts, renderPair(kd))
	}
	right := strings.Join(gparts, "  ")

	return border + "\n " + left + sep + right
}

func (h *Home) layoutMode() string {
	if h.width < layoutBreakpointSingle {
		return "single"
	}
	if h.width < layoutBreakpointDual {
		return "stacked"
	}
	return "dual"
}

func (h *Home) selectedSession() *session.Session {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	return h.flatItems[h.cursor].Session
}

func (h *Home) selectedPreview() (*session.Session, string) {
	s := h.selectedSession()
	if s == nil {
		return nil, ""
	}
	content := h.previewCache[s.ID]
	return s, content
}

// --- Internal helpers ---

func (h *Home) rebuildFlatItems() {
	h.flatItems = BuildFlatItems(h.sessions)
}

func (h *Home) rebuildSessionMap() {
	h.sessionByID = make(map[string]*session.Session, len(h.sessions))
	for _, s := range h.sessions {
		h.sessionByID[s.ID] = s
	}
}

func (h *Home) syncViewport() {
	if len(h.flatItems) == 0 {
		return
	}
	// Ensure cursor is within bounds.
	if h.cursor < 0 {
		h.cursor = 0
	}
	if h.cursor >= len(h.flatItems) {
		h.cursor = len(h.flatItems) - 1
	}
	// Calculate visible height for sidebar.
	contentHeight := h.height - 2 - helpBarHeight
	if contentHeight < 1 {
		contentHeight = 1
	}
	// Scroll to keep cursor visible.
	if h.cursor < h.viewOffset {
		h.viewOffset = h.cursor
	}
	if h.cursor >= h.viewOffset+contentHeight {
		h.viewOffset = h.cursor - contentHeight + 1
	}
}

func (h *Home) loadSessions() tea.Msg {
	rows, err := h.storage.LoadSessions()
	if err != nil {
		return loadSessionsMsg{err: err}
	}

	sessions := make([]*session.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, session.FromRow(row))
	}
	return loadSessionsMsg{sessions: sessions}
}

func (h *Home) setError(err error) {
	h.err = err
	h.errTime = time.Now()
}
