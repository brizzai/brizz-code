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

	"github.com/brizzai/brizz-code/internal/analytics"
	"github.com/brizzai/brizz-code/internal/chrome"
	"github.com/brizzai/brizz-code/internal/config"
	"github.com/brizzai/brizz-code/internal/debuglog"
	"github.com/brizzai/brizz-code/internal/git"
	"github.com/brizzai/brizz-code/internal/github"
	"github.com/brizzai/brizz-code/internal/hooks"
	"github.com/brizzai/brizz-code/internal/naming"
	"github.com/brizzai/brizz-code/internal/session"
	"github.com/brizzai/brizz-code/internal/tmux"
	"github.com/brizzai/brizz-code/internal/workspace"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	tickInterval           = 2 * time.Second
	previewTickInterval    = 500 * time.Millisecond
	previewCacheTTL        = 500 * time.Millisecond
	layoutBreakpointSingle = 50
	layoutBreakpointDual   = 80
	helpBarHeight          = 2 // border line + shortcuts
	statusRoundRobin       = 5 // sessions per tick
)

// Message types.
type (
	tickMsg          time.Time
	hookChangedMsg   struct{} // HookWatcher detected a status file change
	statusUpdateMsg  struct{ attachedSessionID string }
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
	openEditorMsg   struct{ err error }
	openPRMsg       struct{ err error }
	quickApproveMsg struct{ err error }
	spinnerTickMsg  struct{}
	previewTickMsg  time.Time
	focusTickMsg    time.Time
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
	infoMsg     string
	infoTime    time.Time

	newDialog             *NewSessionDialog
	confirmDialog         *ConfirmDialog
	renameDialog          *RenameDialog
	helpOverlay           *HelpOverlay
	settingsDialog        *SettingsDialog
	worktreeDialog        *WorktreeDialog
	createWorkspaceDialog *CreateWorkspaceDialog
	branchDialog          *BranchCheckoutDialog

	pendingWorkspaces []*PendingWorkspace // in-flight workspace creations

	repoExpanded     map[string]bool // repo path -> expanded state
	previewCache     map[string]string
	previewCacheTime map[string]time.Time
	statusRRIndex    int // round-robin index for status updates

	gitInfoCache map[string]*git.RepoInfo // repo root path -> git info
	gitRRIndex   int                      // round-robin index for git refresh
	ghAvailable  bool                     // cached gh CLI availability

	hookWatcher *hooks.HookWatcher

	// Focus mode (split view).
	focusMode     bool
	controlClient *tmux.ControlClient
	cachedSidebar string // cached sidebar render for focus mode
	sidebarDirty  bool   // true when sidebar needs rebuild

	// Filter.
	filterInput  textinput.Model
	filterActive bool
	filterText   string

	// Config.
	cfg     *config.Config
	version string

	// Bug report / diagnostics.
	errorHistory *ErrorHistory
	actionLog    *ActionLog
	bugReport    *BugReportDialog

	// Background worker for async status/git/PR updates.
	statusTrigger chan struct{} // buffered(1), triggers worker
	workerMu      sync.Mutex    // protects sessions/gitInfoCache from concurrent worker access
	ctx           context.Context
	cancel        context.CancelFunc
	workerStarted bool

	startTime time.Time // app start time for uptime tracking

	// Rendering diagnostics (accumulated counters for bug reports).
	renderStats RenderStats
}

// NewHome creates the main TUI model.
func NewHome(storage *session.StateDB, cfg *config.Config, version string) *Home {
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
		worktreeDialog:        NewWorktreeDialog(),
		createWorkspaceDialog: NewCreateWorkspaceDialog(),
		branchDialog:          NewBranchCheckoutDialog(),
		bugReport:             NewBugReportDialog(),
		previewCache:          make(map[string]string),
		previewCacheTime:      make(map[string]time.Time),
		gitInfoCache:          make(map[string]*git.RepoInfo),
		filterInput:           fi,
		cfg:                   cfg,
		version:               version,
		errorHistory:          NewErrorHistory(50),
		actionLog:             NewActionLog(100),
		statusTrigger:         make(chan struct{}, 1),
		ctx:                   ctx,
		cancel:                cancel,
		startTime:             time.Now(),
	}
}

// Init implements tea.Model.
func (h *Home) Init() tea.Cmd {
	return tea.Batch(
		h.loadSessions,
		h.tick(),
		h.previewTick(),
	)
}

// Update implements tea.Model.
func (h *Home) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.renderStats.RecordResize(msg.Width, msg.Height)
		// Only log resizes after the initial one (startup always sends one).
		if h.width > 0 && (msg.Width != h.width || msg.Height != h.height) {
			debuglog.Logger.Info("window resized",
				"from", fmt.Sprintf("%dx%d", h.width, h.height),
				"to", fmt.Sprintf("%dx%d", msg.Width, msg.Height),
				"resize_count", h.renderStats.ResizeCount,
			)
		}
		h.width = msg.Width
		h.height = msg.Height
		h.sidebarDirty = true
		h.newDialog.SetSize(msg.Width, msg.Height)
		h.confirmDialog.SetSize(msg.Width, msg.Height)
		h.renameDialog.SetSize(msg.Width, msg.Height)
		h.helpOverlay.SetSize(msg.Width, msg.Height)
		h.settingsDialog.SetSize(msg.Width, msg.Height)
		h.worktreeDialog.SetSize(msg.Width, msg.Height)
		h.createWorkspaceDialog.SetSize(msg.Width, msg.Height)
		h.branchDialog.SetSize(msg.Width, msg.Height)
		h.bugReport.SetSize(msg.Width, msg.Height)
		h.syncViewport()
		return h, nil

	case tea.KeyMsg:
		return h.handleKey(msg)

	case tickMsg:
		return h.handleTick()

	case hookChangedMsg:
		// HookWatcher detected a status file change. Do immediate hook-only sync.
		h.workerMu.Lock()
		h.syncHookStatuses(h.sessions)
		h.rebuildFlatItems()
		h.workerMu.Unlock()
		return h, h.listenForHookChanges

	case statusUpdateMsg:
		// Returned after detaching from session.
		h.isAttaching.Store(false)
		// Immediate hook sync (data already in HookWatcher from hooks that fired during attach).
		h.workerMu.Lock()
		h.syncHookStatuses(h.sessions)
		h.rebuildFlatItems()
		h.workerMu.Unlock()
		// Also trigger full background refresh for pane captures, git, etc.
		select {
		case h.statusTrigger <- struct{}{}:
		default:
		}
		return h, nil

	case sessionCreateMsg:
		return h.handleSessionCreate(msg)

	case forkSessionMsg:
		s := session.NewSession(msg.title, msg.path)
		s.WorkspaceName = msg.workspaceName
		s.ForkFromID = msg.parentClaudeSessionID
		return h, func() tea.Msg {
			if err := s.Start(); err != nil {
				return sessionCreateResultMsg{err: err}
			}
			return sessionCreateResultMsg{session: s}
		}

	case sessionCreateResultMsg:
		return h.handleSessionCreateResult(msg)

	case sessionDeleteMsg:
		if msg.err != nil {
			h.setError(msg.err)
		} else {
			analytics.Track(analytics.EventSessionDeleted, nil)
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
			if err := h.storage.UpdateStatus(s.ID, string(s.GetStatus())); err != nil {
				debuglog.Logger.Error("storage: UpdateStatus after restart", "id", s.ID, "err", err)
			}
			if err := h.storage.UpdateTmuxSession(s.ID, s.TmuxSessionName); err != nil {
				debuglog.Logger.Error("storage: UpdateTmuxSession after restart", "id", s.ID, "err", err)
			}
		}
		h.rebuildFlatItems()
		return h, nil

	case sessionRenameMsg:
		if s, ok := h.sessionByID[msg.id]; ok {
			s.Title = msg.newTitle
			s.ManuallyRenamed = true
			analytics.Track(analytics.EventSessionRenamed, nil)
			if err := h.storage.UpdateTitle(s.ID, msg.newTitle); err != nil {
				debuglog.Logger.Error("storage: UpdateTitle (rename)", "id", s.ID, "err", err)
			}
			if err := h.storage.MarkManuallyRenamed(s.ID); err != nil {
				debuglog.Logger.Error("storage: MarkManuallyRenamed", "id", s.ID, "err", err)
			}
			h.rebuildFlatItems()
		}
		return h, nil

	case settingsClosedMsg:
		// Re-read tick interval from config after settings change.
		return h, nil

	case bugReportClosedMsg:
		return h, nil

	case bugReportOpenErrMsg:
		h.bugReport.submitting = false
		h.setError(msg.err)
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

	case branchListMsg:
		if msg.err != nil {
			h.branchDialog.ShowError(msg.err.Error())
			return h, nil
		}
		h.branchDialog.Show(msg.branches, msg.repoPath, msg.isDirty, msg.userEmail)
		return h, nil

	case branchCheckoutMsg:
		h.branchDialog.Hide()
		if msg.err != nil {
			h.setError(fmt.Errorf("checkout: %w", msg.err))
			return h, nil
		}
		// Refresh git info for the repo.
		h.workerMu.Lock()
		h.gitInfoCache[msg.repoPath] = git.RefreshGitInfo(msg.repoPath)
		h.workerMu.Unlock()
		h.rebuildFlatItems()
		// Trigger PR refresh for new branch.
		select {
		case h.statusTrigger <- struct{}{}:
		default:
		}
		return h, nil

	case statusSnapshotMsg:
		if msg.err != nil {
			h.setError(fmt.Errorf("snapshot: %w", msg.err))
		} else {
			h.setInfo("Snapshot saved: " + msg.path)
		}
		return h, nil

	case previewMsg:
		h.previewCache[msg.sessionID] = msg.content
		h.previewCacheTime[msg.sessionID] = time.Now()
		return h, nil

	case workspaceListMsg:
		if msg.err != nil {
			h.worktreeDialog.Hide()
			h.setError(fmt.Errorf("worktree list: %w", msg.err))
			return h, nil
		}
		if msg.provider.IsCustom() {
			// Custom provider: go straight to create workspace dialog.
			h.worktreeDialog.Hide()
			h.createWorkspaceDialog.Show(msg.provider, msg.repoPath)
			return h, nil
		}
		h.worktreeDialog.Show(msg.workspaces, h.sessions, msg.provider, msg.repoPath, msg.defaultBranch)
		return h, nil

	case workspaceSelectedMsg:
		return h.handleSessionCreate(sessionCreateMsg{
			path:          msg.info.Path,
			title:         msg.info.Name,
			workspaceName: msg.info.Name,
		})

	case showCreateWorkspaceMsg:
		h.worktreeDialog.Hide()
		h.createWorkspaceDialog.Show(msg.provider, msg.repoPath)
		return h, nil

	case showWorktreeDialogMsg:
		h.createWorkspaceDialog.Hide()
		// Re-fetch worktree list for the same repo.
		if msg.repoPath != "" {
			h.worktreeDialog.ShowLoading()
			return h, tea.Batch(h.fetchWorkspaceListForRepo(msg.repoPath), spinnerTickCmd)
		}
		return h, nil

	case workspaceCreateMsg:
		// Close dialog immediately — creation runs in background.
		h.createWorkspaceDialog.Hide()
		analytics.Track(analytics.EventWorkspaceCreated, map[string]interface{}{
			"provider": func() string {
				if msg.provider.IsCustom() {
					return "shell"
				}
				return "git"
			}(),
		})

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
		baseBranch := msg.baseBranch
		copyClaudeSettings := h.cfg.IsCopyClaudeSettingsEnabled() && !provider.IsCustom()
		return h, tea.Batch(func() tea.Msg {
			info, err := provider.Create(repoPath, name, branch, baseBranch)
			if err == nil && info != nil && copyClaudeSettings {
				copyClaudeSettingsFile(repoPath, info.Path)
			}
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

	case previewTickMsg:
		// Fast preview-only tick — skips status/git work, just refreshes the preview pane.
		if h.focusMode {
			return h, h.previewTick() // focus mode has its own faster tick
		}
		var previewCmd tea.Cmd
		if sel := h.selectedSession(); sel != nil && sel.IsAlive() {
			previewCmd = h.fetchPreview(sel)
		}
		if previewCmd != nil {
			return h, tea.Batch(previewCmd, h.previewTick())
		}
		return h, h.previewTick()

	case focusTickMsg:
		if !h.focusMode {
			return h, nil
		}
		s := h.selectedSession()
		if s == nil || !s.IsAlive() {
			h.focusMode = false
			h.sidebarDirty = true
			return h, nil
		}
		return h, tea.Batch(h.fetchPreviewFresh(s), h.focusTick())

	case spinnerTickMsg:
		// Advance spinner in whichever dialog is active.
		if h.worktreeDialog.IsVisible() && h.worktreeDialog.loading {
			h.worktreeDialog.frame++
			return h, spinnerTickCmd
		}
		if h.branchDialog.IsVisible() && h.branchDialog.loading {
			h.branchDialog.frame++
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

			// Initialize analytics (once, after first load).
			analytics.Init(h.cfg.IsTelemetryEnabled())
			effectiveTheme := h.cfg.Theme
			if effectiveTheme == "" {
				effectiveTheme = "tokyo-night"
			}
			analytics.TrackAppStarted(
				h.version,
				len(h.sessions),
				len(groups),
				effectiveTheme,
				h.cfg.GetEnterMode(),
				h.cfg.IsAutoNameEnabled(),
				h.cfg.IsCopyClaudeSettingsEnabled(),
			)
		}

		// Start listening for hook changes.
		if h.hookWatcher != nil {
			return h, h.listenForHookChanges
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
	if h.bugReport.IsVisible() {
		return h.bugReport.View()
	}
	if h.settingsDialog.IsVisible() {
		return h.settingsDialog.View()
	}
	if h.createWorkspaceDialog.IsVisible() {
		return h.createWorkspaceDialog.View()
	}
	if h.worktreeDialog.IsVisible() {
		return h.worktreeDialog.View()
	}
	if h.branchDialog.IsVisible() {
		return h.branchDialog.View()
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
		preview := RenderPreview(s, content, h.selectedRepoInfo(), h.width, previewHeight, h.focusMode)
		b.WriteString(preview)
	default: // dual
		sidebarWidth := h.width * 35 / 100
		if sidebarWidth < 20 {
			sidebarWidth = 20
		}
		previewWidth := h.width - sidebarWidth - 3 // 3 for separator " │ "

		// In focus mode, reuse cached sidebar to avoid expensive rebuild on every keystroke.
		var leftPanel string
		if h.focusMode && !h.sidebarDirty && h.cachedSidebar != "" {
			leftPanel = h.cachedSidebar
		} else {
			leftPanel = RenderSidebar(h.flatItems, h.sessions, h.gitInfoCache, h.cursor, h.viewOffset, sidebarWidth, contentHeight)
			leftPanel = ensureExactHeight(leftPanel, contentHeight)
			leftPanel = ensureExactWidth(leftPanel, sidebarWidth)
			h.cachedSidebar = leftPanel
			h.sidebarDirty = false
		}

		s, content := h.selectedPreview()
		rightPanel := RenderPreview(s, content, h.selectedRepoInfo(), previewWidth, contentHeight, h.focusMode)

		// Build separator as explicit lines.
		sepColor := ColorBorder
		if h.focusMode {
			sepColor = ColorAccent
		}
		sepStyle := lipgloss.NewStyle().Foreground(sepColor)
		sepLines := make([]string, contentHeight)
		for i := range sepLines {
			sepLines[i] = sepStyle.Render(" │ ")
		}
		separator := strings.Join(sepLines, "\n")

		// Ensure exact dimensions before joining (prevents ANSI misalignment).
		rightPanel = ensureExactHeight(rightPanel, contentHeight)
		rightPanel = ensureExactWidth(rightPanel, previewWidth)

		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, separator, rightPanel))
	}

	// Pad to fill content area.
	lineCount := strings.Count(b.String(), "\n") + 1
	for lineCount < h.height-helpBarHeight {
		b.WriteString("\n")
		lineCount++
	}

	// Focus mode bar / Filter bar / Help bar.
	if h.focusMode {
		border := lipgloss.NewStyle().Foreground(ColorAccent).Render(strings.Repeat("─", h.width))
		b.WriteString("\n")
		b.WriteString(border + "\n")
		b.WriteString(" " + HelpKeyStyle.Render("esc") + " " + HelpDescStyle.Render("Unfocus") + "  " +
			DimStyle.Render("all keys forwarded to session"))
		lineCount += 2 // border + shortcut line
	} else if h.filterActive {
		border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))
		b.WriteString("\n")
		b.WriteString(border + "\n")
		b.WriteString(" " + HelpKeyStyle.Render("/") + " " + h.filterInput.View())
		lineCount += 2
	} else if h.filterText != "" {
		// Show active filter indicator even when not typing.
		border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", h.width))
		b.WriteString("\n")
		b.WriteString(border + "\n")
		b.WriteString(" " + HelpKeyStyle.Render("/") + " " + DimStyle.Render(h.filterText) + "  " + DimStyle.Render("(/ to edit, esc to clear)"))
		lineCount += 2
	} else {
		// Help bar (border + "\n " + shortcuts = 2 lines).
		b.WriteString("\n")
		b.WriteString(h.renderHelpBar())
		lineCount += 2
	}

	// Info/error flash message (most recent wins, overwrites last line).
	showInfo := h.infoMsg != "" && time.Since(h.infoTime) < 5*time.Second
	showErr := h.err != nil && time.Since(h.errTime) < 5*time.Second
	if showInfo && (!showErr || h.infoTime.After(h.errTime)) {
		b.WriteString("\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(" " + h.infoMsg))
		lineCount++
	} else if showErr {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render(" " + h.err.Error()))
		lineCount++
	}

	// Track height mismatches (counter for bug report, log only on first occurrence).
	// Uses incremental lineCount instead of re-scanning the output.
	if h.height > 0 && lineCount != h.height {
		diff := lineCount - h.height
		prevCount := h.renderStats.HeightMismatchCount
		h.renderStats.RecordHeightMismatch(diff)
		if prevCount == 0 {
			debuglog.Logger.Warn("View height mismatch detected",
				"output_lines", lineCount,
				"expected", h.height,
				"diff", diff,
				"layout", h.layoutMode(),
			)
		}
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
	if h.bugReport.IsVisible() {
		dialog, cmd := h.bugReport.Update(msg)
		h.bugReport = dialog
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
	if h.worktreeDialog.IsVisible() {
		dialog, cmd := h.worktreeDialog.Update(msg)
		h.worktreeDialog = dialog
		return h, cmd
	}
	if h.branchDialog.IsVisible() {
		dialog, cmd := h.branchDialog.Update(msg)
		h.branchDialog = dialog
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

	// Focus mode: forward keys to tmux session.
	if h.focusMode {
		return h.handleFocusKey(msg)
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
			return h, h.fetchPreviewForSelected()
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
			if previewCmd := h.fetchPreviewForSelected(); previewCmd != nil {
				return h, tea.Batch(cmd, previewCmd)
			}
			return h, cmd
		}
	}

	switch msg.String() {
	case "j", "down":
		h.cursor = NextSelectableItem(h.flatItems, h.cursor, 1)
		h.syncViewport()
		return h, h.fetchPreviewForSelected()
	case "k", "up":
		h.cursor = NextSelectableItem(h.flatItems, h.cursor, -1)
		h.syncViewport()
		return h, h.fetchPreviewForSelected()
	case "enter":
		// Toggle repo group or attach session.
		if h.cursor >= 0 && h.cursor < len(h.flatItems) && h.flatItems[h.cursor].IsRepoHeader {
			h.toggleRepoGroup()
			return h, nil
		}
		if h.cfg.GetEnterMode() == "split" {
			return h, h.enterFocusMode()
		}
		if s := h.selectedSession(); s != nil {
			h.actionLog.Add("attach session", s.Title, true)
			analytics.Track(analytics.EventSessionAttached, nil)
		}
		return h, h.attachSelected()
	case "tab":
		if h.cursor >= 0 && h.cursor < len(h.flatItems) && h.flatItems[h.cursor].IsRepoHeader {
			return h, nil
		}
		if h.cfg.GetEnterMode() == "split" {
			if s := h.selectedSession(); s != nil {
				h.actionLog.Add("attach session", s.Title, true)
			}
			return h, h.attachSelected()
		}
		return h, h.enterFocusMode()
	case " ":
		// Jump to next waiting (or finished) session.
		h.jumpToNextAttentionSession()
		analytics.Track(analytics.EventSpaceJump, nil)
		return h, h.fetchPreviewForSelected()
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
		h.actionLog.Add("create session", repoPath, true)
		return h.handleSessionCreate(sessionCreateMsg{
			path:  repoPath,
			title: repoName,
		})
	case "n":
		// New session at any repo path.
		h.newDialog.Show()
		return h, nil
	case "w":
		// New worktree session.
		repoPath := h.resolveCurrentRepo()
		if repoPath == "" {
			h.setError(fmt.Errorf("no repo selected"))
			return h, nil
		}
		h.worktreeDialog.ShowLoading()
		return h, tea.Batch(h.fetchWorkspaceListForRepo(repoPath), spinnerTickCmd)
	case "f":
		return h, h.forkSelected()
	case "d":
		if s := h.selectedSession(); s != nil {
			h.actionLog.Add("delete session", s.Title, true)
		}
		return h, h.confirmDeleteSelected()
	case "r":
		if s := h.selectedSession(); s != nil {
			h.actionLog.Add("restart session", s.Title, true)
			analytics.Track(analytics.EventSessionRestarted, nil)
		}
		return h, h.restartSelected()
	case "R":
		return h, h.renameSelected()
	case "e":
		if s := h.selectedSession(); s != nil {
			h.actionLog.Add("open editor", fmt.Sprintf("%q at %s", h.cfg.GetEditor(), s.ProjectPath), true)
			analytics.Track(analytics.EventEditorOpened, map[string]interface{}{"editor": h.cfg.GetEditor()})
		}
		return h, h.openEditorSelected()
	case "p":
		h.actionLog.Add("open PR", "", true)
		analytics.Track(analytics.EventPROpened, nil)
		return h, h.openPRInBrowser()
	case "Y":
		if s := h.selectedSession(); s != nil {
			h.actionLog.Add("quick approve", s.Title, true)
			analytics.Track(analytics.EventQuickApprove, nil)
		}
		return h, h.quickApproveSelected()
	case "b":
		repoPath := h.resolveCurrentRepo()
		if repoPath == "" {
			return h, nil
		}
		h.branchDialog.ShowLoading()
		return h, tea.Batch(h.fetchBranchList(repoPath), spinnerTickCmd)
	case "/":
		h.filterActive = true
		h.filterInput.Focus()
		analytics.Track(analytics.EventFilterUsed, nil)
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
			return h, h.fetchPreviewForSelected()
		}
		return h, nil
	case "S":
		h.settingsDialog.Show()
		analytics.Track(analytics.EventSettingsOpened, nil)
		return h, nil
	case "!":
		h.actionLog.Add("open bug report", "", true)
		h.bugReport.Show(h.version, len(h.sessions), h.errorHistory, h.actionLog, h.width, h.height, &h.renderStats, time.Since(h.startTime))
		analytics.Track(analytics.EventBugReportOpened, nil)
		return h, nil
	case "D":
		s := h.selectedSession()
		if s == nil {
			return h, nil
		}
		h.actionLog.Add("status snapshot", s.Title, true)
		return h, func() tea.Msg {
			return captureStatusSnapshot(s, s.ID)
		}
	case "?":
		h.helpOverlay.Show()
		return h, nil
	case "q", "ctrl+c":
		h.cancel() // stops background worker
		if h.hookWatcher != nil {
			h.hookWatcher.Stop()
		}
		if h.controlClient != nil {
			h.controlClient.Close()
		}
		analytics.Track(analytics.EventAppQuit, map[string]interface{}{
			"uptime_seconds": int(time.Since(h.startTime).Seconds()),
		})
		analytics.Shutdown()
		return h, tea.Quit
	}

	return h, nil
}

// --- Session operations ---

func (h *Home) markSessionAccessed(s *session.Session) {
	s.MarkAccessed()
	if err := h.storage.UpdateLastAccessed(s.ID); err != nil {
		debuglog.Logger.Error("storage: UpdateLastAccessed", "id", s.ID, "err", err)
	}
}

func (h *Home) attachSelected() tea.Cmd {
	if h.cursor < 0 || h.cursor >= len(h.flatItems) || h.flatItems[h.cursor].IsRepoHeader {
		return nil
	}
	s := h.flatItems[h.cursor].Session
	if s == nil || !s.IsAlive() {
		return nil
	}

	h.markSessionAccessed(s)
	s.Acknowledge()
	if err := h.storage.SetAcknowledged(s.ID, true); err != nil {
		debuglog.Logger.Error("storage: SetAcknowledged", "id", s.ID, "err", err)
	}

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
	debuglog.Logger.Info("creating session", "title", msg.title, "path", msg.path)
	s := session.NewSession(msg.title, msg.path)
	s.WorkspaceName = msg.workspaceName
	return h, func() tea.Msg {
		if err := s.Start(); err != nil {
			debuglog.Logger.Error("session Start() failed", "title", msg.title, "path", msg.path, "err", err)
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

	analytics.Track(analytics.EventSessionCreated, nil)

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

	return h, h.fetchPreviewForSelected()
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

	h.markSessionAccessed(s)
	id := s.ID
	title := s.Title
	debuglog.Logger.Info("restarting session", "id", id, "title", title)
	return func() tea.Msg {
		var err error
		if s.IsAlive() && !s.GetTmuxSession().IsPaneDead() {
			// Tmux session alive, just respawn the pane.
			err = s.RespawnClaude()
			if err != nil {
				debuglog.Logger.Error("RespawnClaude failed", "id", id, "err", err)
			}
		} else {
			// Tmux session dead or pane dead — full restart.
			err = s.Restart()
			if err != nil {
				debuglog.Logger.Error("Restart failed", "id", id, "err", err)
			}
		}
		return sessionRestartMsg{id: id, err: err}
	}
}

func (h *Home) forkSelected() tea.Cmd {
	s := h.selectedSession()
	if s == nil {
		h.setError(fmt.Errorf("cannot fork: no session selected"))
		return nil
	}
	if s.ClaudeSessionID == "" {
		h.setError(fmt.Errorf("cannot fork: session has no Claude conversation ID yet"))
		return nil
	}
	title := s.Title + " (fork)"
	claudeSessionID := s.ClaudeSessionID
	path := s.ProjectPath
	workspaceName := s.WorkspaceName
	return func() tea.Msg {
		return forkSessionMsg{
			parentClaudeSessionID: claudeSessionID,
			path:                  path,
			title:                 title,
			workspaceName:         workspaceName,
		}
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
	parts := strings.Fields(h.cfg.GetEditor())
	if len(parts) == 0 {
		return func() tea.Msg {
			return openEditorMsg{err: fmt.Errorf("no editor configured")}
		}
	}
	projectPath := s.ProjectPath
	return func() tea.Msg {
		args := append(parts[1:], projectPath)
		cmd := exec.Command(parts[0], args...)
		if err := cmd.Start(); err != nil {
			debuglog.Logger.Error("editor launch failed", "editor", parts[0], "args", args, "err", err)
			return openEditorMsg{err: err}
		}
		return openEditorMsg{}
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
	h.markSessionAccessed(s)
	ts := s.GetTmuxSession()
	debuglog.Logger.Info("quick approve", "id", s.ID, "title", s.Title)
	return func() tea.Msg {
		// Send "y" then Enter: menu-style prompts ignore "y" and Enter confirms;
		// (Y/n) and (y/N) prompts accept "y" as approval, Enter submits.
		_ = ts.SendKeys("y")
		err := ts.SendKeys("Enter")
		return quickApproveMsg{err: err}
	}
}

// --- Focus mode (split preview) ---

func (h *Home) getControlClient() *tmux.ControlClient {
	if h.controlClient == nil || h.controlClient.IsClosed() {
		cc, err := tmux.NewControlClient()
		if err != nil {
			debuglog.Logger.Error("failed to create control client", "err", err)
			return nil
		}
		h.controlClient = cc
	}
	return h.controlClient
}

func (h *Home) enterFocusMode() tea.Cmd {
	s := h.selectedSession()
	if s == nil || !s.IsAlive() {
		h.setError(fmt.Errorf("cannot focus: session not running"))
		return nil
	}
	h.focusMode = true
	h.sidebarDirty = true // separator color changes
	h.actionLog.Add("focus preview", s.Title, true)
	return h.focusTick()
}

func (h *Home) focusTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return focusTickMsg(t)
	})
}

func (h *Home) handleFocusKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := h.selectedSession()
	if s == nil || !s.IsAlive() {
		h.focusMode = false
		h.sidebarDirty = true
		return h, nil
	}

	if msg.Type == tea.KeyEsc {
		h.focusMode = false
		h.sidebarDirty = true
		h.actionLog.Add("unfocus preview", s.Title, true)
		return h, nil
	}

	cc := h.getControlClient()
	if cc == nil {
		h.setError(fmt.Errorf("failed to connect to tmux"))
		h.focusMode = false
		h.sidebarDirty = true
		return h, nil
	}

	target := s.GetTmuxSession().Name

	switch msg.Type {
	case tea.KeyEnter:
		cc.SendKeys(target, "Enter")
	case tea.KeyBackspace:
		cc.SendKeys(target, "BSpace")
	case tea.KeyTab:
		cc.SendKeys(target, "Tab")
	case tea.KeySpace:
		cc.SendKeys(target, "Space")
	case tea.KeyUp:
		cc.SendKeys(target, "Up")
	case tea.KeyDown:
		cc.SendKeys(target, "Down")
	case tea.KeyLeft:
		cc.SendKeys(target, "Left")
	case tea.KeyRight:
		cc.SendKeys(target, "Right")
	case tea.KeyHome:
		cc.SendKeys(target, "Home")
	case tea.KeyEnd:
		cc.SendKeys(target, "End")
	case tea.KeyPgUp:
		cc.SendKeys(target, "PageUp")
	case tea.KeyPgDown:
		cc.SendKeys(target, "PageDown")
	case tea.KeyDelete:
		cc.SendKeys(target, "DC")
	case tea.KeyCtrlC:
		cc.SendKeys(target, "C-c")
	case tea.KeyCtrlD:
		cc.SendKeys(target, "C-d")
	case tea.KeyCtrlA:
		cc.SendKeys(target, "C-a")
	case tea.KeyCtrlU:
		cc.SendKeys(target, "C-u")
	case tea.KeyCtrlL:
		cc.SendKeys(target, "C-l")
	case tea.KeyCtrlW:
		cc.SendKeys(target, "C-w")
	case tea.KeyCtrlK:
		cc.SendKeys(target, "C-k")
	case tea.KeyRunes:
		cc.SendLiteralKeys(target, string(msg.Runes))
	default:
		if str := msg.String(); str != "" {
			cc.SendLiteralKeys(target, str)
		}
	}
	return h, nil
}

func (h *Home) fetchPreviewFresh(s *session.Session) tea.Cmd {
	id := s.ID
	ts := s.GetTmuxSession()
	return func() tea.Msg {
		content, _ := ts.CapturePaneFresh()
		return previewMsg{sessionID: id, content: content}
	}
}

func (h *Home) openPRInBrowser() tea.Cmd {
	repo := h.resolveCurrentRepo()
	if repo == "" {
		debuglog.Logger.Debug("openPR: no repo selected")
		h.setError(fmt.Errorf("no repo selected"))
		return nil
	}

	info := h.gitInfoCache[repo]
	if info == nil || info.PR == nil || info.PR.URL == "" {
		debuglog.Logger.Debug("openPR: no PR for branch", "repo", repo)
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
				debuglog.Logger.Error("failed to open PR in browser", "url", prURL, "err", openErr)
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

	debuglog.Logger.Info("deleting session", "id", id, "title", s.Title)

	// Kill tmux session if alive.
	if s.IsAlive() {
		if err := s.Kill(); err != nil {
			debuglog.Logger.Error("failed to kill tmux session", "id", id, "err", err)
		}
	}

	// Remove from storage.
	if err := h.storage.DeleteSession(id); err != nil {
		debuglog.Logger.Error("failed to delete session from storage", "id", id, "err", err)
	}

	// Remove hook status file.
	if err := os.Remove(filepath.Join(hooks.GetHooksDir(), id+".json")); err != nil && !os.IsNotExist(err) {
		debuglog.Logger.Error("failed to remove hook status file", "id", id, "err", err)
	}

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

func (h *Home) previewTick() tea.Cmd {
	return tea.Tick(previewTickInterval, func(t time.Time) tea.Msg {
		return previewTickMsg(t)
	})
}

// listenForHookChanges blocks until the HookWatcher signals a status change,
// then returns a hookChangedMsg. Runs as a tea.Cmd in its own goroutine.
func (h *Home) listenForHookChanges() tea.Msg {
	if h.hookWatcher == nil {
		return nil
	}
	select {
	case <-h.hookWatcher.Changes():
		return hookChangedMsg{}
	case <-h.ctx.Done():
		return nil
	}
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

	// Preview is now handled by the faster previewTick, no need to fetch here.
	return h, h.tick()
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

// syncHookStatuses reads the latest hook statuses from the HookWatcher and applies
// them to the given sessions. Caller must ensure thread-safe access to sessions.
func (h *Home) syncHookStatuses(sessions []*session.Session) {
	if h.hookWatcher == nil {
		return
	}
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
				if err := h.storage.UpdateClaudeSessionID(s.ID, s.ClaudeSessionID); err != nil {
					debuglog.Logger.Error("storage: UpdateClaudeSessionID", "id", s.ID, "err", err)
				}
			}
			// Persist prompt changes and reset title on every new prompt
			// (for non-manually-renamed, non-Claude-named sessions).
			if s.PromptCount != oldPromptCount {
				h.markSessionAccessed(s)
				if err := h.storage.UpdatePromptCount(s.ID, s.PromptCount); err != nil {
					debuglog.Logger.Error("storage: UpdatePromptCount", "id", s.ID, "err", err)
				}
				if h.cfg.IsAutoNameEnabled() && s.TitleGenerated && !s.ManuallyRenamed && s.ClaudeSessionName == "" {
					s.TitleGenerated = false
					if err := h.storage.ResetTitleGenerated(s.ID); err != nil {
						debuglog.Logger.Error("storage: ResetTitleGenerated", "id", s.ID, "err", err)
					}
				}
			}
			if s.FirstPrompt != "" && s.FirstPrompt != oldFirstPrompt {
				if err := h.storage.UpdateFirstPrompt(s.ID, s.FirstPrompt); err != nil {
					debuglog.Logger.Error("storage: UpdateFirstPrompt", "id", s.ID, "err", err)
				}
			}
		}
	}
}

func (h *Home) statusWorkerCycle() {
	// Recover from panics to keep the worker alive.
	defer func() {
		if r := recover(); r != nil {
			debuglog.Logger.Error("statusWorkerCycle panic recovered", "panic", r)
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
	h.syncHookStatuses(sessions)

	// 3b. Auto-name: generate title for ONE session per cycle.
	// Priority: manual (R key) > Claude session name > last prompt heuristic.
	if h.cfg.IsAutoNameEnabled() {
		for _, s := range sessions {
			if s.ManuallyRenamed {
				continue
			}

			// Periodically re-read Claude's session name from JSONL (~every 30s per session).
			if s.ClaudeSessionID != "" && time.Since(s.ClaudeNameLastChecked) > 30*time.Second {
				s.ClaudeNameLastChecked = time.Now()
				name := session.ReadClaudeSessionName(s.ClaudeSessionID, s.ProjectPath)
				if name != "" && name != s.ClaudeSessionName {
					s.ClaudeSessionName = name
					s.Title = name
					if err := h.storage.UpdateTitle(s.ID, name); err != nil {
						debuglog.Logger.Error("storage: UpdateTitle (claude name)", "id", s.ID, "err", err)
					}
					s.TitleGenerated = true
					if err := h.storage.MarkTitleGenerated(s.ID); err != nil {
						debuglog.Logger.Error("storage: MarkTitleGenerated", "id", s.ID, "err", err)
					}
				}
			}
			if s.ClaudeSessionName != "" {
				continue
			}

			// Fallback: prompt-based title heuristic.
			if s.FirstPrompt != "" && !s.TitleGenerated {
				title := naming.GenerateTitle(s.FirstPrompt)
				if title != "" && title != s.Title {
					s.Title = title
					if err := h.storage.UpdateTitle(s.ID, title); err != nil {
						debuglog.Logger.Error("storage: UpdateTitle (auto-name)", "id", s.ID, "err", err)
					}
				}
				s.TitleGenerated = true
				if err := h.storage.MarkTitleGenerated(s.ID); err != nil {
					debuglog.Logger.Error("storage: MarkTitleGenerated", "id", s.ID, "err", err)
				}
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
			if err := h.storage.UpdateStatus(s.ID, string(newStatus)); err != nil {
				debuglog.Logger.Error("storage: UpdateStatus", "id", s.ID, "status", newStatus, "err", err)
			}
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

func (h *Home) fetchBranchList(repoPath string) tea.Cmd {
	return func() tea.Msg {
		branches, err := git.ListBranches(repoPath)
		isDirty := git.HasUncommittedChanges(repoPath)
		userEmail := git.GetUserEmail(repoPath)
		return branchListMsg{branches: branches, repoPath: repoPath, isDirty: isDirty, userEmail: userEmail, err: err}
	}
}

func (h *Home) fetchWorkspaceListForRepo(repoPath string) tea.Cmd {
	return func() tea.Msg {
		provider := workspace.ResolveProvider(repoPath)
		workspaces, err := provider.List(repoPath)
		defaultBranch := git.GetDefaultBranch(repoPath)
		return workspaceListMsg{workspaces: workspaces, provider: provider, repoPath: repoPath, defaultBranch: defaultBranch, err: err}
	}
}

// copyClaudeSettingsFile copies .claude/settings.local.json from srcRepo to dstRepo.
func copyClaudeSettingsFile(srcRepo, dstRepo string) {
	srcFile := filepath.Join(srcRepo, ".claude", "settings.local.json")
	data, err := os.ReadFile(srcFile)
	if err != nil {
		return // source doesn't exist, nothing to copy
	}
	dstDir := filepath.Join(dstRepo, ".claude")
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		debuglog.Logger.Error("copyClaudeSettings: failed to create .claude dir", "dst", dstDir, "err", err)
		return
	}
	if err := os.WriteFile(filepath.Join(dstDir, "settings.local.json"), data, 0600); err != nil {
		debuglog.Logger.Error("copyClaudeSettings: failed to write settings file", "dst", dstRepo, "err", err)
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

// fetchPreviewForSelected returns a tea.Cmd that fetches the preview for the
// currently selected session, or nil if no live session is selected.
func (h *Home) fetchPreviewForSelected() tea.Cmd {
	sel := h.selectedSession()
	if sel == nil || !sel.IsAlive() {
		return nil
	}
	return h.fetchPreview(sel)
}

// --- Rendering helpers ---

func (h *Home) renderHeader() string {
	statusCounts := make(map[session.Status]int)
	for _, s := range h.sessions {
		statusCounts[s.GetStatus()]++
	}

	bg := ColorSurface
	logo := lipgloss.NewStyle().Foreground(ColorBrand).Background(bg).Bold(true).Render(">_")
	title := logo + lipgloss.NewStyle().Background(bg).Render(" ") + TitleStyle.Background(bg).Render("brizz-code")

	// Build status indicators — only show non-zero.
	var indicators []string
	if n := statusCounts[session.StatusRunning] + statusCounts[session.StatusStarting]; n > 0 {
		indicators = append(indicators, StatusRunningStyle.Background(bg).Render(fmt.Sprintf("● %d running", n)))
	}
	if n := statusCounts[session.StatusWaiting]; n > 0 {
		indicators = append(indicators, StatusWaitingStyle.Background(bg).Render(fmt.Sprintf("◐ %d waiting", n)))
	}
	if n := statusCounts[session.StatusFinished]; n > 0 {
		indicators = append(indicators, StatusFinishedStyle.Background(bg).Render(fmt.Sprintf("● %d finished", n)))
	}
	if n := statusCounts[session.StatusIdle]; n > 0 {
		indicators = append(indicators, StatusIdleStyle.Background(bg).Render(fmt.Sprintf("○ %d idle", n)))
	}
	if n := statusCounts[session.StatusError]; n > 0 {
		indicators = append(indicators, StatusErrorStyle.Background(bg).Render(fmt.Sprintf("✕ %d error", n)))
	}

	sep := lipgloss.NewStyle().Foreground(ColorBorder).Background(bg).Render(" • ")
	stats := strings.Join(indicators, sep)

	sp := lipgloss.NewStyle().Background(bg).Render
	content := title + sp("  ") + stats

	// Manually pad to full width with background-styled spaces to avoid ANSI reset issues.
	if h.width > 0 {
		contentWidth := lipgloss.Width(content)
		if contentWidth < h.width {
			content += sp(strings.Repeat(" ", h.width-contentWidth))
		}
	}
	return HeaderBarStyle.Render(content)
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
	h.sidebarDirty = true
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
	prevOffset := h.viewOffset
	// Scroll to keep cursor visible.
	if h.cursor < h.viewOffset {
		h.viewOffset = h.cursor
	}
	if h.cursor >= h.viewOffset+contentHeight {
		h.viewOffset = h.cursor - contentHeight + 1
	}
	if h.viewOffset != prevOffset {
		h.renderStats.RecordViewportDrift()
	}
}

func (h *Home) loadSessions() tea.Msg {
	rows, err := h.storage.LoadSessions()
	if err != nil {
		debuglog.Logger.Error("failed to load sessions from database", "err", err)
		return loadSessionsMsg{err: err}
	}

	sessions := make([]*session.Session, 0, len(rows))
	for _, row := range rows {
		sessions = append(sessions, session.FromRow(row))
	}

	// Demo mode: only show sessions under the specified path prefix.
	if prefix := os.Getenv("BRIZZ_DEMO_PREFIX"); prefix != "" {
		filtered := make([]*session.Session, 0, len(sessions))
		for _, s := range sessions {
			if strings.HasPrefix(s.ProjectPath, prefix) {
				filtered = append(filtered, s)
			}
		}
		sessions = filtered
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
	if err != nil {
		h.errorHistory.Add(err.Error())
		analytics.Track(analytics.EventErrorOccurred, map[string]interface{}{
			"category": strings.SplitN(err.Error(), ":", 2)[0],
		})
	}
}

func (h *Home) setInfo(msg string) {
	h.infoMsg = msg
	h.infoTime = time.Now()
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
