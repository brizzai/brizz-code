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

// Messages for workspace picker flow.
type (
	workspaceListMsg struct {
		workspaces []workspace.WorkspaceInfo
		provider   workspace.Provider
		repoPath   string
		err        error
	}
	workspaceSelectedMsg struct {
		info workspace.WorkspaceInfo
	}
	showCreateWorkspaceMsg struct {
		provider workspace.Provider
		repoPath string
	}
	showCustomPathMsg      struct{}
	showWorkspacePickerMsg struct {
		repoPath string
	}
)

type pickerItem struct {
	workspace *workspace.WorkspaceInfo // nil for action items
	action    string                   // "create" or "custom" for action items
}

// WorkspacePickerDialog shows available workspaces and action items.
type WorkspacePickerDialog struct {
	visible       bool
	width, height int
	workspaces    []workspace.WorkspaceInfo
	filtered      []pickerItem
	cursor        int
	filterInput   textinput.Model
	loading       bool
	err           string
	sessionCounts map[string]int // projectPath -> count of existing sessions
	canCreate     bool
	provider      workspace.Provider
	repoPath      string
}

// NewWorkspacePickerDialog creates a new workspace picker.
func NewWorkspacePickerDialog() *WorkspacePickerDialog {
	fi := textinput.New()
	fi.Placeholder = "type to filter"
	fi.CharLimit = 64
	fi.Width = 30

	return &WorkspacePickerDialog{
		filterInput:   fi,
		sessionCounts: make(map[string]int),
	}
}

// Show populates and shows the picker.
func (d *WorkspacePickerDialog) Show(workspaces []workspace.WorkspaceInfo, sessions []*session.Session, provider workspace.Provider, repoPath string) {
	d.visible = true
	d.workspaces = workspaces
	d.provider = provider
	d.repoPath = repoPath
	d.canCreate = provider.CanCreate()
	d.cursor = 0
	d.err = ""
	d.loading = false
	d.filterInput.SetValue("")
	d.filterInput.Focus()

	// Build session counts by project path.
	d.sessionCounts = make(map[string]int)
	for _, s := range sessions {
		d.sessionCounts[s.ProjectPath]++
	}

	d.rebuildFiltered()
}

// ShowLoading shows the picker in loading state.
func (d *WorkspacePickerDialog) ShowLoading() {
	d.visible = true
	d.loading = true
	d.err = ""
}

// ShowError shows an error in the picker.
func (d *WorkspacePickerDialog) ShowError(err string) {
	d.loading = false
	d.err = err
}

func (d *WorkspacePickerDialog) Hide() {
	d.visible = false
	d.filterInput.Blur()
}

func (d *WorkspacePickerDialog) IsVisible() bool { return d.visible }

func (d *WorkspacePickerDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *WorkspacePickerDialog) rebuildFiltered() {
	filter := strings.ToLower(strings.TrimSpace(d.filterInput.Value()))
	d.filtered = nil

	for i := range d.workspaces {
		ws := &d.workspaces[i]
		if filter != "" && !strings.Contains(strings.ToLower(ws.Name), filter) {
			continue
		}
		d.filtered = append(d.filtered, pickerItem{workspace: ws})
	}

	// Action items always shown.
	if d.canCreate {
		d.filtered = append(d.filtered, pickerItem{action: "create"})
	}
	d.filtered = append(d.filtered, pickerItem{action: "custom"})

	// Clamp cursor.
	if d.cursor >= len(d.filtered) {
		d.cursor = len(d.filtered) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
}

// Update handles key events.
func (d *WorkspacePickerDialog) Update(msg tea.Msg) (*WorkspacePickerDialog, tea.Cmd) {
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
	case "up", "k":
		if d.cursor > 0 {
			d.cursor--
		}
		return d, nil
	case "down", "j":
		if d.cursor < len(d.filtered)-1 {
			d.cursor++
		}
		return d, nil
	case "enter":
		if d.cursor < 0 || d.cursor >= len(d.filtered) {
			return d, nil
		}
		item := d.filtered[d.cursor]
		d.Hide()
		if item.workspace != nil {
			info := *item.workspace
			return d, func() tea.Msg { return workspaceSelectedMsg{info: info} }
		}
		switch item.action {
		case "create":
			provider := d.provider
			repoPath := d.repoPath
			return d, func() tea.Msg { return showCreateWorkspaceMsg{provider: provider, repoPath: repoPath} }
		case "custom":
			return d, func() tea.Msg { return showCustomPathMsg{} }
		}
		return d, nil
	}

	// Route to filter input.
	var cmd tea.Cmd
	d.filterInput, cmd = d.filterInput.Update(msg)
	d.rebuildFiltered()
	return d, cmd
}

// View renders the picker dialog.
func (d *WorkspacePickerDialog) View() string {
	var b strings.Builder

	// Title with repo name.
	title := "New Session"
	if d.repoPath != "" {
		title += " — " + filepath.Base(d.repoPath)
	}
	b.WriteString(TitleStyle.Render(title))
	b.WriteString("\n\n")

	if d.loading {
		b.WriteString(DimStyle.Render("  Loading workspaces..."))
		b.WriteString("\n\n")
		b.WriteString(DimStyle.Render("esc: cancel"))
		return d.wrapDialog(b.String())
	}

	if d.err != "" {
		b.WriteString(ErrorStyle.Render("  " + d.err))
		b.WriteString("\n\n")
		b.WriteString(DimStyle.Render("esc: cancel"))
		return d.wrapDialog(b.String())
	}

	// Filter input.
	b.WriteString("  " + DimStyle.Render("/") + " " + d.filterInput.View())
	b.WriteString("\n\n")

	// Items.
	maxVisible := 10
	for i, item := range d.filtered {
		if i >= maxVisible {
			remaining := len(d.filtered) - maxVisible
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ... %d more", remaining)))
			b.WriteString("\n")
			break
		}

		selected := i == d.cursor
		prefix := "  "
		if selected {
			prefix = SessionSelectionPrefix.Render("▸ ")
		}

		if item.workspace != nil {
			b.WriteString(d.renderWorkspaceRow(item.workspace, selected))
		} else {
			// Separator before action items.
			if i > 0 && d.filtered[i-1].workspace != nil {
				b.WriteString(DimStyle.Render("  " + strings.Repeat("─", 30)))
				b.WriteString("\n")
			}
			switch item.action {
			case "create":
				label := "+ Create workspace..."
				if selected {
					b.WriteString(prefix + SessionTitleSelStyle.Render(label))
				} else {
					b.WriteString(prefix + lipgloss.NewStyle().Foreground(ColorAccent).Render(label))
				}
			case "custom":
				label := "~ Custom path..."
				if selected {
					b.WriteString(prefix + SessionTitleSelStyle.Render(label))
				} else {
					b.WriteString(prefix + DimStyle.Render(label))
				}
			}
		}
		b.WriteString("\n")
	}

	if len(d.filtered) == 0 {
		b.WriteString(DimStyle.Render("  No workspaces found"))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(DimStyle.Render("↑↓ select  enter: create  esc: cancel"))

	return d.wrapDialog(b.String())
}

func (d *WorkspacePickerDialog) renderWorkspaceRow(ws *workspace.WorkspaceInfo, selected bool) string {
	prefix := "  "
	if selected {
		prefix = SessionSelectionPrefix.Render("▸ ")
	}

	// Name.
	name := ws.Name
	if len(name) > 16 {
		name = name[:16]
	}

	// Branch.
	branch := ws.Branch
	if len(branch) > 16 {
		branch = branch[:13] + "..."
	}

	// Session count for this workspace path.
	count := d.sessionCounts[ws.Path]

	if selected {
		line := fmt.Sprintf("%-16s", name)
		if branch != "" {
			line += "  " + branch
		}
		if count > 0 {
			line += fmt.Sprintf("  %d", count)
		}
		return prefix + SessionTitleSelStyle.Render(line)
	}

	nameStyled := lipgloss.NewStyle().Foreground(ColorText).Render(fmt.Sprintf("%-16s", name))
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

func (d *WorkspacePickerDialog) wrapDialog(content string) string {
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
