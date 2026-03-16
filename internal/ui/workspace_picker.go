package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/session"
	"github.com/yuvalhayke/brizz-code/internal/workspace"
)

// Messages for workspace/worktree flow.
type (
	workspaceListMsg struct {
		workspaces    []workspace.WorkspaceInfo
		provider      workspace.Provider
		repoPath      string
		defaultBranch string
		err           error
	}
	workspaceSelectedMsg struct {
		info workspace.WorkspaceInfo
	}
	showCreateWorkspaceMsg struct {
		provider workspace.Provider
		repoPath string
	}
	showWorktreeDialogMsg struct {
		repoPath string
	}
)

type worktreeFocus int

const (
	focusBranchInput worktreeFocus = iota
	focusWorktreeList
)

// WorktreeDialog shows branch input + existing worktrees for creating new worktree sessions.
type WorktreeDialog struct {
	visible       bool
	width, height int
	branchInput   textinput.Model
	workspaces    []workspace.WorkspaceInfo
	cursor        int // cursor in the worktree list
	focus         worktreeFocus
	loading       bool
	err           string
	frame         int
	repoPath      string
	provider      workspace.Provider
	sessionCounts map[string]int
	defaultBranch string
}

// NewWorktreeDialog creates a new worktree dialog.
func NewWorktreeDialog() *WorktreeDialog {
	bi := textinput.New()
	bi.Placeholder = "branch name (e.g. feature/login)"
	bi.CharLimit = 128
	bi.Width = 40

	return &WorktreeDialog{
		branchInput:   bi,
		sessionCounts: make(map[string]int),
	}
}

// Show populates and shows the dialog.
func (d *WorktreeDialog) Show(workspaces []workspace.WorkspaceInfo, sessions []*session.Session, provider workspace.Provider, repoPath, defaultBranch string) {
	d.visible = true
	d.workspaces = workspaces
	d.provider = provider
	d.repoPath = repoPath
	d.defaultBranch = defaultBranch
	d.cursor = 0
	d.focus = focusBranchInput
	d.err = ""
	d.loading = false
	d.branchInput.SetValue(defaultBranch)
	d.branchInput.Focus()
	d.branchInput.CursorEnd()

	// Build session counts by project path.
	d.sessionCounts = make(map[string]int)
	for _, s := range sessions {
		d.sessionCounts[s.ProjectPath]++
	}
}

// ShowLoading shows the dialog in loading state.
func (d *WorktreeDialog) ShowLoading() {
	d.visible = true
	d.loading = true
	d.err = ""
	d.frame = 0
}

// ShowError shows an error in the dialog.
func (d *WorktreeDialog) ShowError(err string) {
	d.loading = false
	d.err = err
}

func (d *WorktreeDialog) Hide() {
	d.visible = false
	d.branchInput.Blur()
}

func (d *WorktreeDialog) IsVisible() bool { return d.visible }

func (d *WorktreeDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
}

// Update handles key events.
func (d *WorktreeDialog) Update(msg tea.Msg) (*WorktreeDialog, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	if d.loading {
		if keyMsg.String() == "esc" {
			d.Hide()
		}
		return d, nil
	}

	switch keyMsg.String() {
	case "esc":
		d.Hide()
		return d, nil

	case "down":
		if d.focus == focusBranchInput && len(d.workspaces) > 0 {
			d.focus = focusWorktreeList
			d.cursor = 0
			d.branchInput.Blur()
			return d, nil
		}
		if d.focus == focusWorktreeList && d.cursor < len(d.workspaces)-1 {
			d.cursor++
			return d, nil
		}
		return d, nil

	case "up":
		if d.focus == focusWorktreeList {
			if d.cursor > 0 {
				d.cursor--
			} else {
				d.focus = focusBranchInput
				d.branchInput.Focus()
			}
			return d, nil
		}
		return d, nil

	case "enter":
		if d.focus == focusWorktreeList && d.cursor >= 0 && d.cursor < len(d.workspaces) {
			// Select existing worktree.
			info := d.workspaces[d.cursor]
			d.Hide()
			return d, func() tea.Msg { return workspaceSelectedMsg{info: info} }
		}
		// Branch input — create new worktree.
		branch := strings.TrimSpace(d.branchInput.Value())
		if branch == "" {
			d.err = "Branch cannot be empty"
			return d, nil
		}
		d.err = ""
		name := workspace.SanitizeBranchName(branch)
		provider := d.provider
		repoPath := d.repoPath
		d.Hide()
		return d, func() tea.Msg {
			return workspaceCreateMsg{name: name, branch: branch, repoPath: repoPath, provider: provider}
		}
	}

	// Route to branch input when focused.
	if d.focus == focusBranchInput {
		var cmd tea.Cmd
		d.branchInput, cmd = d.branchInput.Update(msg)
		return d, cmd
	}

	return d, nil
}

// View renders the worktree dialog.
func (d *WorktreeDialog) View() string {
	var b strings.Builder

	// Title with repo name.
	title := "New Worktree"
	if d.repoPath != "" {
		title += " — " + filepath.Base(d.repoPath)
	}
	b.WriteString(TitleStyle.Render(title))
	b.WriteString("\n\n")

	if d.loading {
		spinner := spinnerFrames[d.frame%len(spinnerFrames)]
		b.WriteString(lipgloss.NewStyle().Foreground(ColorAccent).Render("  "+spinner) + DimStyle.Render(" Loading worktrees..."))
		b.WriteString("\n\n")
		b.WriteString(DimStyle.Render("esc: cancel"))
		return d.wrapDialog(b.String())
	}

	if d.err != "" && d.focus == focusBranchInput {
		// Show error near the input.
	}

	// Branch input.
	b.WriteString(DimStyle.Render("Branch:"))
	b.WriteString("\n")
	b.WriteString(d.branchInput.View())
	b.WriteString("\n")

	// Path preview.
	branch := strings.TrimSpace(d.branchInput.Value())
	if branch != "" && d.focus == focusBranchInput {
		name := workspace.SanitizeBranchName(branch)
		preview := workspace.DeriveWorktreePathPreview(d.repoPath, name)
		b.WriteString(DimStyle.Render("  → " + preview))
		b.WriteString("\n")
	}

	if d.err != "" {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render("  " + d.err))
		b.WriteString("\n")
	}

	// Existing worktrees.
	if len(d.workspaces) > 0 {
		b.WriteString("\n")
		b.WriteString(DimStyle.Render("Existing worktrees:"))
		b.WriteString("\n")
		for i, ws := range d.workspaces {
			selected := d.focus == focusWorktreeList && i == d.cursor
			b.WriteString(d.renderWorktreeRow(&ws, selected))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(DimStyle.Render("enter: create  ↓ existing  esc: cancel"))

	return d.wrapDialog(b.String())
}

func (d *WorktreeDialog) renderWorktreeRow(ws *workspace.WorkspaceInfo, selected bool) string {
	prefix := "  "
	if selected {
		prefix = SessionSelectionPrefix.Render("▸ ")
	}

	// Name.
	name := ws.Name
	if len(name) > 20 {
		name = name[:20]
	}

	// Branch.
	branch := ws.Branch
	if len(branch) > 16 {
		branch = branch[:13] + "..."
	}

	// Session count.
	count := d.sessionCounts[ws.Path]

	if selected {
		line := fmt.Sprintf("%-20s", name)
		if branch != "" {
			line += "  " + branch
		}
		if count > 0 {
			line += fmt.Sprintf("  %d", count)
		}
		return prefix + SessionTitleSelStyle.Render(line)
	}

	nameStyled := lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("%-20s", name))
	var parts []string
	parts = append(parts, prefix+nameStyled)
	if branch != "" {
		parts = append(parts, BranchStyle.Render(branch))
	}
	if count > 0 {
		parts = append(parts, DimStyle.Render(fmt.Sprintf("%d", count)))
	}
	return strings.Join(parts, "  ")
}

func (d *WorktreeDialog) wrapDialog(content string) string {
	dialogWidth := d.width - 4
	if dialogWidth > 64 {
		dialogWidth = 64
	}
	if dialogWidth < 30 {
		dialogWidth = 30
	}

	box := DialogStyle.Width(dialogWidth).Render(content)
	return lipgloss.Place(d.width, d.height, lipgloss.Center, lipgloss.Center, box)
}
