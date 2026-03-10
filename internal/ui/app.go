package ui

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/chrome"
	"github.com/yuvalhayke/brizz-code/internal/config"
	"github.com/yuvalhayke/brizz-code/internal/debuglog"
	"github.com/yuvalhayke/brizz-code/internal/git"
	"github.com/yuvalhayke/brizz-code/internal/github"
	"github.com/yuvalhayke/brizz-code/internal/hooks"
	"github.com/yuvalhayke/brizz-code/internal/naming"
	"github.com/yuvalhayke/brizz-code/internal/session"
	"github.com/yuvalhayke/brizz-code/internal/tmux"
	"github.com/yuvalhayke/brizz-code/internal/workspace"
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
	tickMsg         time.Time
	statusUpdateMsg struct{ attachedSessionID string }
	sessionDeleteMsg struct {
		id               string
		err              error
		destroyWorkspace bool
		workspaceName    string
	}
	sessionRestartMsg struct {
		id  string
		err error
	}
	sessionCreateResultMsg struct {
		session *session.Session
		err     error
	}
	previewMsg struct {
		sessionID string
		content   string
	}
	loadSessionsMsg struct {
		sessions    []*session.Session
		ghAvailable bool
		warning     string
		err         error
	}
	openEditorMsg    struct{ err error }
	openPRMsg        struct{ err error }
	quickApproveMsg  struct{ err error }
	spinnerTickMsg struct{}
)

func spinnerTickCmd() tea.Msg {
	time.Sleep(100 * time.Millisecond)
	return spinnerTickMsg{}
}

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

	newDialog             *NewSessionDialog
	confirmDialog         *ConfirmDialog
	renameDialog          *RenameDialog
	helpOverlay           *HelpOverlay
	settingsDialog        *SettingsDialog
	workspacePicker       *WorkspacePickerDialog
	createWorkspaceDialog *CreateWorkspaceDialog

	pendingWorkspaces []*PendingWorkspace // in-flight workspace creations

	repoExpanded     map[string]bool // repo path -> expanded state
	previewCache     map[string]string
	previewCacheTime map[string]time.Time
	statusRRIndex    int // round-robin index for status updates

	gitInfoCache map[string]*git.RepoInfo // repo root path -> git info
	gitRRIndex   int                      // round-robin index for git refresh
	ghAvailable  bool                     // cached gh CLI availability

	hookWatcher *hooks.HookWatcher

	// Filter.
	filterInput  textinput.Model
	filterActive bool
	filterText   string

	// Config.
	cfg *config.Config

	// Background worker for async status/git/PR updates.
	statusTrigger chan struct{} // buffered(1), triggers worker
	workerMu      sync.Mutex   // protects sessions/gitInfoCache from concurrent worker access
	ctx           context.Context
	cancel        context.CancelFunc
	workerStarted bool

}

// NewHome creates the main TUI model.
func NewHome(storage *session.StateDB, cfg *config.Config) *Home {
	ctx, cancel := context.WithCancel(context.Background())

	fi := textinput.New()
	fi.Placeholder = "filter..."
	fi.CharLimit = 64
	fi.Width = 20

	// Apply theme from config if set.
	if cfg.Theme != "" {
		ApplyPalette(PaletteByName(cfg.Theme))
	}

	return &Home{
		storage:               storage,
		sessionByID:           make(map[string]*session.Session),
		repoExpanded:          make(map[string]bool),
		newDialog:             NewNewSessionDialog(),
		confirmDialog:         NewConfirmDialog(),
		renameDialog:          NewRenameDialog(),
		helpOverlay:           NewHelpOverlay(),
		settingsDialog:        NewSettingsDialog(cfg),
		workspacePicker:       NewWorkspacePickerDialog(),
		createWorkspaceDialog: NewCreateWorkspaceDialog(),
		previewCache:          make(map[string]string),
		previewCacheTime:      make(map[string]time.Time),
		gitInfoCache:          make(map[string]*git.RepoInfo),
		filterInput:           fi,
		cfg:                   cfg,
		statusTrigger:         make(chan struct{}, 1),
		ctx:                   ctx,
		cancel:                cancel,
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
		h.renameDialog.SetSize(msg.Width, msg.Height)
		h.helpOverlay.SetSize(msg.Width, msg.Height)
		h.settingsDialog.SetSize(msg.Width, msg.Height)
		h.workspacePicker.SetSize(msg.Width, msg.Height)
		h.createWorkspaceDialog.SetSize(msg.Width, msg.Height)
		h.syncViewport()
		return h, nil

	case tea.KeyMsg:
		return h.handleKey(msg)

	case tickMsg:
		return h.handleTick()

	case statusUpdateMsg:
		// Returned after detaching from session.
		h.isAttaching.Store(false)
		// Trigger immediate background refresh.
		select {
		case h.statusTrigger <- struct{}{}:
		default:
		}
		h.rebuildFlatItems()
		return h, nil

	case sessionCreateMsg:
		return h.handleSessionCreate(msg)

	case sessionCreateResultMsg:
		return h.handleSessionCreateResult(msg)

	case sessionDeleteMsg:
		if msg.err != nil {
			h.setError(msg.err)
		} else {
			// Resolve provider before deleting session (need project path).
			var destroyProvider workspace.Provider
			var destroyRepoPath string
			if msg.destroyWorkspace && msg.workspaceName != "" {
				if s, ok := h.sessionByID[msg.id]; ok {
					destroyRepoPath = session.GetRepoRoot(s.ProjectPath)
					destroyProvider = workspace.ResolveProvider(destroyRepoPath)
				}
			}
			h.deleteSession(msg.id)
			// If workspace destroy requested, do it async.
			if destroyProvider != nil && destroyProvider.CanDestroy() {
				wsName := msg.workspaceName
				repoPath := destroyRepoPath
				sid := msg.id
				return h, func() tea.Msg {
					err := destroyProvider.Destroy(repoPath, wsName)
					return workspaceDestroyResultMsg{sessionID: sid, err: err}
				}
			}
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

	case sessionRenameMsg:
		if s, ok := h.sessionByID[msg.id]; ok {
			s.Title = msg.newTitle
			s.ManuallyRenamed = true
			_ = h.storage.UpdateTitle(s.ID, msg.newTitle)
			_ = h.storage.MarkManuallyRenamed(s.ID)
			h.rebuildFlatItems()
		}
		return h, nil

	case settingsClosedMsg:
		// Re-read tick interval from config after settings change.
		return h, nil

	case openEditorMsg:
		if msg.err != nil {
			h.setError(fmt.Errorf("editor: %w", msg.err))
		}
		return h, nil

	case openPRMsg:
		if msg.err != nil {
			h.setError(msg.err)
		}
		return h, nil

	case quickApproveMsg:
		if msg.err != nil {
			h.setError(fmt.Errorf("approve: %w", msg.err))
		}
		return h, nil

	case previewMsg:
		h.previewCache[msg.sessionID] = msg.content
		h.previewCacheTime[msg.sessionID] = time.Now()
		return h, nil

	case workspaceListMsg:
		if msg.err != nil {
			h.workspacePicker.Hide()
			h.setError(fmt.Errorf("workspace list: %w", msg.err))
			// Fall back to path dialog.
			h.newDialog.Show()
			return h, nil
		}
		h.workspacePicker.Show(msg.workspaces, h.sessions, msg.provider, msg.repoPath)
		return h, nil

	case workspaceSelectedMsg:
		return h.handleSessionCreate(sessionCreateMsg{
			path:          msg.info.Path,
			title:         msg.info.Name,
			workspaceName: msg.info.Name,
		})

	case showCreateWorkspaceMsg:
		h.workspacePicker.Hide()
		h.createWorkspaceDialog.Show(msg.provider, msg.repoPath)
		return h, nil

	case showCustomPathMsg:
		h.workspacePicker.Hide()
		h.newDialog.Show()
		return h, nil

	case showWorkspacePickerMsg:
		h.createWorkspaceDialog.Hide()
		// Re-fetch workspace list for the same repo.
		if msg.repoPath != "" {
			h.workspacePicker.ShowLoading()
			return h, tea.Batch(h.fetchWorkspaceListForRepo(msg.repoPath), spinnerTickCmd)
		}
		return h, nil

	case workspaceCreateMsg:
		// Close dialog immediately — creation runs in background.
		h.createWorkspaceDialog.Hide()

		pw := &PendingWorkspace{
			ID:       generatePendingID(),
			Name:     msg.name,
			RepoPath: msg.repoPath,
		}
		h.pendingWorkspaces = append(h.pendingWorkspaces, pw)

		// Expand the repo group and rebuild sidebar.
		h.repoExpanded[msg.repoPath] = true
		h.rebuildFlatItems()

		// Auto-select the phantom entry.
		for i, item := range h.flatItems {
			if item.Pending != nil && item.Pending.ID == pw.ID {
				h.cursor = i
				h.syncViewport()
				break
			}
		}

		pendingID := pw.ID
		provider := msg.provider
		repoPath := msg.repoPath
		name := msg.name
		branch := msg.branch
		return h, tea.Batch(func() tea.Msg {
			info, err := provider.Create(repoPath, name, branch)
			return workspaceCreateResultMsg{info: info, err: err, pendingID: pendingID, repoPath: repoPath}
		}, spinnerTickCmd)

	case workspaceCreateResultMsg:
		h.removePendingWorkspace(msg.pendingID)

		if msg.err != nil {
			h.setError(fmt.Errorf("workspace create failed: %w", msg.err))
			h.rebuildFlatItems()
			// Clamp cursor if it was on the removed phantom.
			if h.cursor >= len(h.flatItems) && len(h.flatItems) > 0 {
				h.cursor = len(h.flatItems) - 1
			}
			return h, nil
		}
		return h.handleSessionCreate(sessionCreateMsg{
			path:          msg.info.Path,
			title:         msg.info.Name,
			workspaceName: msg.info.Name,
		})

	case workspaceDestroyResultMsg:
		if msg.err != nil {
			h.setError(fmt.Errorf("workspace destroy: %w", msg.err))
		}
		return h, nil

	case spinnerTickMsg:
		// Advance spinner in whichever dialog is active.
		if h.workspacePicker.IsVisible() && h.workspacePicker.loading {
			h.workspacePicker.frame++
			return h, spinnerTickCmd
		}
		if h.createWorkspaceDialog.IsVisible() && h.createWorkspaceDialog.creating {
			h.createWorkspaceDialog.frame++
			return h, spinnerTickCmd
		}
		// Animate pending workspace spinners in sidebar.
		if len(h.pendingWorkspaces) > 0 {
			for _, pw := range h.pendingWorkspaces {
				pw.Frame++
			}
			return h, spinnerTickCmd
		}
		return h, nil

	case loadSessionsMsg:
		if msg.err != nil {
			h.setError(msg.err)
			return h, nil
		}
		if msg.warning != "" {
			h.setError(fmt.Errorf("%s", msg.warning))
		}
		h.sessions = msg.sessions
		h.rebuildSessionMap()
		// Default all repos to expanded on first load.
		groups := session.GroupByRepo(h.sessions)
		for repo := range groups {
			if _, exists := h.repoExpanded[repo]; !exists {
				h.repoExpanded[repo] = true
			}
		}
		h.ghAvailable = msg.ghAvailable
		h.rebuildFlatItems()
		if len(h.flatItems) > 0 && h.cursor == 0 {
			h.cursor = FirstSelectableItem(h.flatItems)
		}

		// Start hook watcher.
		if h.hookWatcher == nil {
			if watcher, err := hooks.NewHookWatcher(); err == nil {
				h.hookWatcher = watcher
				go watcher.Start()
			}
		}

		// Start background status worker (once).
		if !h.workerStarted {
			h.workerStarted = true
			go h.statusWorker()
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
		return lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render("   brizz-code")
	}

	// Modals take priority.
	if h.helpOverlay.IsVisible() {
		return h.helpOverlay.View()
	}
	if h.settingsDialog.IsVisible() {
		return h.settingsDialog.View()
	}
	if h.createWorkspaceDialog.IsVisible() {
		return h.createWorkspaceDialog.View()
	}
	if h.workspacePicker.IsVisible() {
		return h.workspacePicker.View()
	}
	if h.newDialog.IsVisible() {
		return h.newDialog.View()
	}
	if h.confirmDialog.IsVisible() {
		return h.confirmDialog.View()
	}
	if h.renameDialog.IsVisible() {
		return h.renameDialog.View()
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
		sidebar := RenderSidebar(h.flatItems, h.sessions, h.gitInfoCache, h.cursor, h.viewOffset, h.width, contentHeight)
		b.WriteString(sidebar)
	case "stacked":
		sidebarHeight := (contentHeight * 55) / 100
		if sidebarHeight < 3 {
			sidebarHeight = 3
		}
		previewHeight := contentHeight - sidebarHeight - 1 // 1 for separator
		sidebar := RenderSidebar(h.flatItems, h.sessions, h.gitInfoCache, h.cursor, h.viewOffset, h.width, sidebarHeight)
		b.WriteString(sidebar)
		b.WriteString("\n")
		b.WriteString(DimStyle.Render(strings.Repeat("─", h.width)))
		b.WriteString("\n")
		s, content := h.selectedPreview()
		preview := RenderPreview(s, content, h.selectedRepoInfo(), h.width, previewHeight)
		b.WriteString(preview)
	default: // dual
		sidebarWidth := h.width * 35 / 100
		if sidebarWidth < 20 {
			sidebarWidth = 20
		}
		previewWidth := h.width - sidebarWidth - 3 // 3 for separator " │ "

		leftPanel := RenderSidebar(h.flatItems, h.sessions, h.gitInfoCache, h.cursor, h.viewOffset, sidebarWidth, contentHeight)
		s, content := h.selectedPreview()
		rightPanel := RenderPreview(s, content, h.selectedRepoInfo(), previewWidth, contentHeight)

		// Build separator as explicit lines.
		sepStyle := lipgloss.NewStyle().Foreground(ColorBorder)
		sepLines := make([]string, contentHeight)
		for i := range sepLines {
			sepLines[i] = sepStyle.Render(" │ ")
		}
		separator := strings.Join(sepLines, "\n")

		// Ensure exact dimensions before joining (prevents ANSI misalignment).
		leftPanel = ensureExactHeight(leftPanel, contentHeight)
		rightPanel = ensureExactHeight(rightPanel, contentHeight)
		leftPanel = ensureExactWidth(leftPanel, sidebarWidth)
		rightPanel = ensureExactWidth(rightPanel, previewWidth)

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, separator, rightPanel))
	}

	// Pad to fill content area.
	lines := strings.Count(b.String(), "\n") + 1
	for lines < h.height-helpBarHeight {
		b.WriteString("\n")
		lines++
	}

	// Filter bar (when active, replaces help bar).
	if h.filterActive {
		border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))
		b.WriteString("\n")
		b.WriteString(border + "\n")
		b.WriteString(" " + HelpKeyStyle.Render("/") + " " + h.filterInput.View())
	} else if h.filterText != "" {
		// Show active filter indicator even when not typing.
		border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))
		b.WriteString("\n")
		b.WriteString(border + "\n")
		b.WriteString(" " + HelpKeyStyle.Render("/") + " " + DimStyle.Render(h.filterText) + "  " + DimStyle.Render("(/ to edit, esc to clear)"))
	} else {
		// Help bar.
		b.WriteString("\n")
		b.WriteString(h.renderHelpBar())
	}

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
	if h.settingsDialog.IsVisible() {
		dialog, cmd := h.settingsDialog.Update(msg)
		h.settingsDialog = dialog
		return h, cmd
	}
	if h.createWorkspaceDialog.IsVisible() {
		dialog, cmd := h.createWorkspaceDialog.Update(msg)
		h.createWorkspaceDialog = dialog
		return h, cmd
	}
	if h.workspacePicker.IsVisible() {
		picker, cmd := h.workspacePicker.Update(msg)
		h.workspacePicker = picker
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
	if h.renameDialog.IsVisible() {
		dialog, cmd := h.renameDialog.Update(msg)
		h.renameDialog = dialog
		return h, cmd
	}

	// Filter mode: route keys to filter input.
	if h.filterActive {
		switch msg.String() {
		case "esc":
			h.filterActive = false
			h.filterText = ""
			h.filterInput.SetValue("")
			h.filterInput.Blur()
			h.rebuildFlatItems()
			// Reset cursor to first item.
			if len(h.flatItems) > 0 {
				h.cursor = FirstSelectableItem(h.flatItems)
			}
			h.syncViewport()
			return h, nil
		case "enter":
			// Accept filter and exit filter mode.
			h.filterActive = false
			h.filterInput.Blur()
			return h, nil
		default:
			var cmd tea.Cmd
			h.filterInput, cmd = h.filterInput.Update(msg)
			h.filterText = h.filterInput.Value()
			h.rebuildFlatItems()
			// Reset cursor when filter changes.
			if len(h.flatItems) > 0 {
				h.cursor = FirstSelectableItem(h.flatItems)
			} else {
				h.cursor = 0
			}
			h.syncViewport()
			return h, cmd
		}
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
		// Toggle repo group or attach session.
		if h.cursor >= 0 && h.cursor < len(h.flatItems) && h.flatItems[h.cursor].IsRepoHeader {
			h.toggleRepoGroup()
			return h, nil
		}
		return h, h.attachSelected()
	case " ":
		// Jump to next waiting (or finished) session.
		h.jumpToNextAttentionSession()
		return h, nil
	case "left", "h":
		h.collapseRepoAtCursor()
		return h, nil
	case "right", "l":
		h.expandRepoAtCursor()
		return h, nil
	case "a":
		// Instant session at current repo path.
		repoPath := h.resolveCurrentRepo()
		if repoPath == "" {
			h.newDialog.Show()
			return h, nil
		}
		repoName := filepath.Base(repoPath)
		return h.handleSessionCreate(sessionCreateMsg{
			path:  repoPath,
			title: repoName,
		})
	case "n":
		// Workspace/worktree picker.
		repoPath := h.resolveCurrentRepo()
		if repoPath == "" {
			h.newDialog.Show()
			return h, nil
		}
		h.workspacePicker.ShowLoading()
		return h, tea.Batch(h.fetchWorkspaceListForRepo(repoPath), spinnerTickCmd)
	case "d":
		return h, h.confirmDeleteSelected()
	case "r":
		return h, h.restartSelected()
	case "R":
		return h, h.renameSelected()
	case "e":
		return h, h.openEditorSelected()
	case "p":
		return h, h.openPRInBrowser()
	case "Y":
		return h, h.quickApproveSelected()
	case "/":
		h.filterActive = true
		h.filterInput.Focus()
		return h, nil
	case "esc":
		// Clear active filter.
		if h.filterText != "" {
			h.filterText = ""
			h.filterInput.SetValue("")
			h.rebuildFlatItems()
			if len(h.flatItems) > 0 {
				h.cursor = FirstSelectableItem(h.flatItems)
			}
			h.syncViewport()
			return h, nil
		}
		return h, nil
	case "S":
		h.settingsDialog.Show()
		return h, nil
	case "?":
		h.helpOverlay.Show()
		return h, nil
	case "q", "ctrl+c":
		h.cancel() // stops background worker
		if h.hookWatcher != nil {
			h.hookWatcher.Stop()
		}
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
	if _, err := exec.LookPath("claude"); err != nil {
		h.setError(fmt.Errorf("claude CLI not found — install Claude Code to create sessions"))
		return h, nil
	}
	s := session.NewSession(msg.title, msg.path)
	s.WorkspaceName = msg.workspaceName
	return h, func() tea.Msg {
		if err := s.Start(); err != nil {
			return sessionCreateResultMsg{err: err}
		}
		return sessionCreateResultMsg{session: s}
	}
}

func (h *Home) handleSessionCreateResult(msg sessionCreateResultMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		h.setError(fmt.Errorf("failed to start session: %w", msg.err))
		return h, nil
	}

	s := msg.session
	h.workerMu.Lock()
	h.sessions = append(h.sessions, s)
	h.rebuildSessionMap()
	h.workerMu.Unlock()

	// Ensure the repo group is expanded for the new session.
	repo := session.GetRepoRoot(s.ProjectPath)
	h.repoExpanded[repo] = true
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
	wsName := s.WorkspaceName

	if wsName != "" {
		repoPath := session.GetRepoRoot(s.ProjectPath)
		provider := workspace.ResolveProvider(repoPath)
		if provider.CanDestroy() {
			h.confirmDialog.ShowDangerWithWorkspace("Delete Session?", s.Title, []string{
				"tmux session terminated",
				"Terminal history lost",
			}, wsName, func() tea.Msg {
				return sessionDeleteMsg{id: id}
			}, func() tea.Msg {
				return sessionDeleteMsg{id: id, destroyWorkspace: true, workspaceName: wsName}
			})
			return nil
		}
	}
	h.confirmDialog.ShowDanger("Delete Session?", s.Title, []string{
		"tmux session terminated",
		"Terminal history lost",
	}, func() tea.Msg {
		return sessionDeleteMsg{id: id}
	})
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

func (h *Home) toggleRepoGroup() {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || !h.flatItems[h.cursor].IsRepoHeader {
		return
	}
	repo := h.flatItems[h.cursor].RepoPath
	h.repoExpanded[repo] = !h.repoExpanded[repo]
	h.rebuildFlatItems()
	// Keep cursor on the same repo header.
	for i, item := range h.flatItems {
		if item.IsRepoHeader && item.RepoPath == repo {
			h.cursor = i
			break
		}
	}
	h.syncViewport()
}

func (h *Home) expandRepoAtCursor() {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) {
		return
	}
	item := h.flatItems[h.cursor]
	var repo string
	if item.IsRepoHeader {
		repo = item.RepoPath
	} else if item.Session != nil {
		repo = session.GetRepoRoot(item.Session.ProjectPath)
	} else {
		return
	}
	if h.repoExpanded[repo] {
		return // Already expanded.
	}
	h.repoExpanded[repo] = true
	h.rebuildFlatItems()
	h.syncViewport()
}

func (h *Home) collapseRepoAtCursor() {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) {
		return
	}
	item := h.flatItems[h.cursor]
	var repo string
	if item.IsRepoHeader {
		repo = item.RepoPath
	} else if item.Session != nil {
		repo = session.GetRepoRoot(item.Session.ProjectPath)
	} else {
		return
	}
	if !h.repoExpanded[repo] {
		return // Already collapsed.
	}
	h.repoExpanded[repo] = false
	h.rebuildFlatItems()
	// Move cursor to the repo header.
	for i, fi := range h.flatItems {
		if fi.IsRepoHeader && fi.RepoPath == repo {
			h.cursor = i
			break
		}
	}
	h.syncViewport()
}


// jumpToNextAttentionSession cycles through sessions needing attention:
// waiting first, then finished. Wraps around, auto-expands collapsed groups.
func (h *Home) jumpToNextAttentionSession() {
	// Build ordered list of ALL sessions (same order as sidebar).
	groups := session.GroupByRepo(h.sessions)
	repos := make([]string, 0, len(groups))
	for repo := range groups {
		repos = append(repos, repo)
	}
	sort.Strings(repos)

	type candidate struct {
		s    *session.Session
		repo string
	}
	var allSessions []candidate
	for _, repo := range repos {
		for _, s := range groups[repo] {
			allSessions = append(allSessions, candidate{s: s, repo: repo})
		}
	}
	if len(allSessions) == 0 {
		return
	}

	// Find the current session's position in allSessions.
	var currentID string
	if h.cursor >= 0 && h.cursor < len(h.flatItems) && !h.flatItems[h.cursor].IsRepoHeader {
		if s := h.flatItems[h.cursor].Session; s != nil {
			currentID = s.ID
		}
	}
	currentIdx := -1
	for i, c := range allSessions {
		if c.s.ID == currentID {
			currentIdx = i
			break
		}
	}

	// findNext scans forward (wrapping) for a session with the given status.
	findNext := func(status session.Status) *candidate {
		n := len(allSessions)
		start := currentIdx + 1
		for i := 0; i < n; i++ {
			c := &allSessions[(start+i)%n]
			if c.s.GetStatus() == status {
				return c
			}
		}
		return nil
	}

	// Priority: waiting > finished.
	target := findNext(session.StatusWaiting)
	if target == nil {
		target = findNext(session.StatusFinished)
	}
	if target == nil {
		return // Silent no-op.
	}

	// Expand the repo group if collapsed.
	h.repoExpanded[target.repo] = true
	h.rebuildFlatItems()

	// Set cursor to the target session.
	for i, item := range h.flatItems {
		if !item.IsRepoHeader && item.Session != nil && item.Session.ID == target.s.ID {
			h.cursor = i
			h.syncViewport()
			return
		}
	}
}

func (h *Home) renameSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil {
		return nil
	}
	h.renameDialog.Show(s.ID, s.Title)
	return nil
}

func (h *Home) openEditorSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil {
		return nil
	}
	editor := h.cfg.GetEditor()
	projectPath := s.ProjectPath
	return func() tea.Msg {
		cmd := exec.Command(editor, projectPath)
		err := cmd.Start()
		return openEditorMsg{err: err}
	}
}

func (h *Home) quickApproveSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil || !s.IsAlive() {
		return nil
	}
	if s.GetStatus() != session.StatusWaiting {
		h.setError(fmt.Errorf("session not waiting for approval"))
		return nil
	}
	ts := s.GetTmuxSession()
	return func() tea.Msg {
		// Send "y" then Enter: menu-style prompts ignore "y" and Enter confirms;
		// (Y/n) and (y/N) prompts accept "y" as approval, Enter submits.
		_ = ts.SendKeys("y")
		err := ts.SendKeys("Enter")
		return quickApproveMsg{err: err}
	}
}

func (h *Home) openPRInBrowser() tea.Cmd {
	repo := h.resolveCurrentRepo()
	if repo == "" {
		h.setError(fmt.Errorf("no repo selected"))
		return nil
	}

	info := h.gitInfoCache[repo]
	if info == nil || info.PR == nil || info.PR.URL == "" {
		h.setError(fmt.Errorf("no PR for this branch"))
		return nil
	}

	prURL := info.PR.URL
	repoName := filepath.Base(repo)

	return func() tea.Msg {
		// Try Chrome extension first.
		client := &chrome.Client{}
		cmd := &chrome.Command{
			ID:     fmt.Sprintf("pr-%d", time.Now().UnixNano()),
			Action: chrome.ActionOpenOrFocus,
			URL:    prURL,
			Group:  repoName,
		}

		_, err := client.Send(cmd)
		if err != nil {
			// Fallback to macOS open command.
			debuglog.Logger.Debug("chrome extension unavailable, falling back to open", "err", err)
			if openErr := exec.Command("open", prURL).Start(); openErr != nil {
				return openPRMsg{err: fmt.Errorf("open PR: %w", openErr)}
			}
		}
		return openPRMsg{}
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

	// Remove hook status file.
	_ = os.Remove(filepath.Join(hooks.GetHooksDir(), id+".json"))

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
	interval := tickInterval
	if h.cfg != nil && h.cfg.TickIntervalSec > 0 {
		interval = time.Duration(h.cfg.TickIntervalSec) * time.Second
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (h *Home) handleTick() (tea.Model, tea.Cmd) {
	// Trigger background worker (non-blocking).
	select {
	case h.statusTrigger <- struct{}{}:
	default: // worker busy, skip
	}

	// Read worker results under lock and rebuild.
	h.workerMu.Lock()
	h.rebuildFlatItems()
	h.workerMu.Unlock()

	// Fetch preview for selected session (already async via tea.Cmd).
	var previewCmd tea.Cmd
	if sel := h.selectedSession(); sel != nil && sel.IsAlive() {
		if _, ok := h.previewCacheTime[sel.ID]; !ok || time.Since(h.previewCacheTime[sel.ID]) > previewCacheTTL {
			previewCmd = h.fetchPreview(sel)
		}
	}

	cmds := []tea.Cmd{h.tick()}
	if previewCmd != nil {
		cmds = append(cmds, previewCmd)
	}
	return h, tea.Batch(cmds...)
}

// statusWorker runs in its own goroutine, performing all blocking I/O
// (tmux, git, gh) outside the Bubble Tea Update() loop.
func (h *Home) statusWorker() {
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.statusTrigger:
		case <-ticker.C:
		}

		h.statusWorkerCycle()
	}
}

func (h *Home) statusWorkerCycle() {
	// Recover from panics to keep the worker alive.
	defer func() {
		if r := recover(); r != nil {
			_ = r // Swallow panic, worker continues next cycle.
		}
	}()

	// 1. Refresh tmux session cache (blocking but in background).
	tmux.RefreshSessionCache()

	// 2. Take a snapshot of sessions under lock.
	h.workerMu.Lock()
	sessions := make([]*session.Session, len(h.sessions))
	copy(sessions, h.sessions)
	h.workerMu.Unlock()

	if len(sessions) == 0 {
		return
	}

	// 3. Sync hook status (fast: in-memory map lookups).
	if h.hookWatcher != nil {
		for _, s := range sessions {
			hs := h.hookWatcher.GetStatus(s.ID)
			if hs != nil {
				oldClaudeSessionID := s.ClaudeSessionID
				oldFirstPrompt := s.FirstPrompt
				oldPromptCount := s.PromptCount
				s.UpdateHookStatus(&session.HookStatus{
					Status:      hs.Status,
					SessionID:   hs.SessionID,
					UpdatedAt:   hs.UpdatedAt,
					UserPrompt:  hs.UserPrompt,
					PromptCount: hs.PromptCount,
				})
				// Persist new Claude session ID if it changed.
				if s.ClaudeSessionID != oldClaudeSessionID && s.ClaudeSessionID != "" {
					_ = h.storage.UpdateClaudeSessionID(s.ID, s.ClaudeSessionID)
				}
				// Persist prompt changes and trigger retitle at threshold.
				if s.PromptCount != oldPromptCount {
					_ = h.storage.UpdatePromptCount(s.ID, s.PromptCount)
					if h.cfg.IsAutoNameEnabled() && s.PromptCount >= naming.RetitlePromptThreshold && s.TitleGenerated && !s.ManuallyRenamed {
						s.TitleGenerated = false
						_ = h.storage.ResetTitleGenerated(s.ID)
					}
				}
				if s.FirstPrompt != "" && s.FirstPrompt != oldFirstPrompt {
					_ = h.storage.UpdateFirstPrompt(s.ID, s.FirstPrompt)
				}
			} else {
				debuglog.Logger.Debug("worker: no hook data for session", "session", s.ID)
			}
		}
	}

	// 3b. Auto-name: generate title for ONE session per cycle.
	if h.cfg.IsAutoNameEnabled() {
		for _, s := range sessions {
			if s.FirstPrompt != "" && !s.ManuallyRenamed && !s.TitleGenerated {
				title := naming.GenerateTitle(s.FirstPrompt)
				if title != "" {
					s.Title = title
					_ = h.storage.UpdateTitle(s.ID, title)
				}
				s.TitleGenerated = true
				_ = h.storage.MarkTitleGenerated(s.ID)
				break // one per cycle
			}
		}
	}

	// 4. Round-robin status updates (pane capture — blocking).
	count := statusRoundRobin
	if count > len(sessions) {
		count = len(sessions)
	}
	for i := 0; i < count; i++ {
		idx := (h.statusRRIndex + i) % len(sessions)
		s := sessions[idx]
		oldStatus := s.GetStatus()
		s.UpdateStatus()
		newStatus := s.GetStatus()
		if oldStatus != newStatus {
			_ = h.storage.UpdateStatus(s.ID, string(newStatus))
		}
	}
	h.statusRRIndex = (h.statusRRIndex + count) % len(sessions)

	// 5. Git info refresh: 1 repo per cycle (round-robin).
	repos := h.uniqueRepoPathsFromSessions(sessions)
	if len(repos) > 0 {
		idx := h.gitRRIndex % len(repos)
		repo := repos[idx]

		info := git.RefreshGitInfo(repo)

		// Preserve PR data unless TTL expired.
		h.workerMu.Lock()
		if old, ok := h.gitInfoCache[repo]; ok && old.PR != nil {
			info.PR = old.PR
			info.LastPRRefresh = old.LastPRRefresh
		}
		h.workerMu.Unlock()

		if h.ghAvailable && (info.LastPRRefresh.IsZero() || time.Since(info.LastPRRefresh) > 60*time.Second) {
			git.RefreshPRInfo(info, repo)
		}

		h.workerMu.Lock()
		h.gitInfoCache[repo] = info
		h.workerMu.Unlock()

		h.gitRRIndex++
	}
}

// uniqueRepoPathsFromSessions returns distinct repo root paths from the given sessions.
func (h *Home) uniqueRepoPathsFromSessions(sessions []*session.Session) []string {
	seen := make(map[string]bool)
	var repos []string
	for _, s := range sessions {
		root := session.GetRepoRoot(s.ProjectPath)
		if !seen[root] {
			seen[root] = true
			repos = append(repos, root)
		}
	}
	return repos
}

func (h *Home) resolveCurrentRepo() string {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) {
		return ""
	}
	item := h.flatItems[h.cursor]
	if item.IsRepoHeader {
		return item.RepoPath
	}
	if item.Session != nil {
		return session.GetRepoRoot(item.Session.ProjectPath)
	}
	return ""
}

func (h *Home) fetchWorkspaceListForRepo(repoPath string) tea.Cmd {
	return func() tea.Msg {
		provider := workspace.ResolveProvider(repoPath)
		workspaces, err := provider.List(repoPath)
		return workspaceListMsg{workspaces: workspaces, provider: provider, repoPath: repoPath, err: err}
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

	logo := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("✦")
	title := logo + " " + TitleStyle.Render("brizz-code")

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
	stats := strings.Join(indicators, sep)

	content := " " + title + "  " + stats
	return HeaderBarStyle.Width(h.width).Render(content)
}

func (h *Home) renderHelpBar() string {
	// Border line.
	border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))

	contextKeys, globalKeys := HelpBarBindings()

	var parts []string
	for _, kd := range contextKeys {
		parts = append(parts, HelpKeyStyle.Render(kd.Key)+" "+HelpDescStyle.Render(kd.Desc))
	}
	sep := HelpSepStyle.Render(" │ ")
	left := strings.Join(parts, "  ")

	var gparts []string
	for _, kd := range globalKeys {
		gparts = append(gparts, HelpKeyStyle.Render(kd.Key)+" "+HelpDescStyle.Render(kd.Desc))
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

func (h *Home) selectedRepoInfo() *git.RepoInfo {
	s := h.selectedSession()
	if s == nil {
		return nil
	}
	repo := session.GetRepoRoot(s.ProjectPath)
	return h.gitInfoCache[repo]
}

// --- Internal helpers ---

func (h *Home) rebuildFlatItems() {
	h.flatItems = BuildFlatItems(h.sessions, h.pendingWorkspaces, h.repoExpanded, h.filterText)
}

func (h *Home) removePendingWorkspace(id string) {
	for i, pw := range h.pendingWorkspaces {
		if pw.ID == id {
			h.pendingWorkspaces = append(h.pendingWorkspaces[:i], h.pendingWorkspaces[i+1:]...)
			return
		}
	}
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
	// Calculate visible height for sidebar (subtract title + underline).
	contentHeight := h.height - 2 - helpBarHeight - 2
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

	// These block but run in the tea.Cmd goroutine, not Update().
	configDir := hooks.GetClaudeConfigDir()
	hooks.InjectClaudeHooks(configDir)
	chrome.InstallNativeMessagingHost()
	ghAvailable := github.IsGHAvailable()

	// Check for claude CLI availability.
	var warning string
	if _, err := exec.LookPath("claude"); err != nil {
		warning = "claude CLI not found — install Claude Code to create sessions"
	}

	return loadSessionsMsg{sessions: sessions, ghAvailable: ghAvailable, warning: warning}
}

func (h *Home) setError(err error) {
	h.err = err
	h.errTime = time.Now()
}

// ensureExactHeight pads or truncates content to exactly n lines.
func ensureExactHeight(content string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	for len(lines) < n {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}

// ensureExactWidth pads or truncates each line to exactly the given visual width.
func ensureExactWidth(content string, width int) string {
	if width <= 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	result := make([]string, len(lines))
	for i, line := range lines {
		w := lipgloss.Width(line)
		if w == width {
			result[i] = line
		} else if w < width {
			result[i] = line + strings.Repeat(" ", width-w)
		} else {
			truncated := lipgloss.NewStyle().MaxWidth(width).Render(line)
			tw := lipgloss.Width(truncated)
			if tw < width {
				truncated += strings.Repeat(" ", width-tw)
			}
			result[i] = truncated
		}
	}
	return strings.Join(result, "\n")
}

