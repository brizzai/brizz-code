package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/yuvalhayke/brizz-code/internal/workspace"
)

// Messages for workspace creation flow.
type (
	workspaceCreateMsg struct {
		name, branch string
	}
	workspaceCreateResultMsg struct {
		info *workspace.WorkspaceInfo
		err  error
	}
	workspaceDestroyResultMsg struct {
		sessionID string
		err       error
	}
)

// CreateWorkspaceDialog handles workspace creation input.
type CreateWorkspaceDialog struct {
	visible     bool
	width       int
	height      int
	nameInput   textinput.Model
	branchInput textinput.Model
	focusIndex  int // 0=name, 1=branch
	creating    bool
	err         string
}

// NewCreateWorkspaceDialog creates a new create workspace dialog.
func NewCreateWorkspaceDialog() *CreateWorkspaceDialog {
	ni := textinput.New()
	ni.Placeholder = "workspace name"
	ni.CharLimit = 64
	ni.Width = 40
	ni.Focus()

	bi := textinput.New()
	bi.Placeholder = "branch (optional)"
	bi.CharLimit = 128
	bi.Width = 40

	return &CreateWorkspaceDialog{
		nameInput:   ni,
		branchInput: bi,
	}
}

func (d *CreateWorkspaceDialog) Show() {
	d.visible = true
	d.creating = false
	d.err = ""
	d.focusIndex = 0
	d.nameInput.SetValue("")
	d.branchInput.SetValue("")
	d.nameInput.Focus()
	d.branchInput.Blur()
}

func (d *CreateWorkspaceDialog) Hide() {
	d.visible = false
	d.nameInput.Blur()
	d.branchInput.Blur()
}

func (d *CreateWorkspaceDialog) IsVisible() bool { return d.visible }

func (d *CreateWorkspaceDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
}

// SetCreating sets the creating state (shows spinner).
func (d *CreateWorkspaceDialog) SetCreating(creating bool) {
	d.creating = creating
}

// SetError sets an error message and re-enables inputs.
func (d *CreateWorkspaceDialog) SetError(err string) {
	d.err = err
	d.creating = false
}

func (d *CreateWorkspaceDialog) updateFocus() {
	if d.focusIndex == 0 {
		d.nameInput.Focus()
		d.branchInput.Blur()
	} else {
		d.nameInput.Blur()
		d.branchInput.Focus()
	}
}

// Update handles key events.
func (d *CreateWorkspaceDialog) Update(msg tea.Msg) (*CreateWorkspaceDialog, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	if d.creating {
		// Only allow esc during creation.
		if keyMsg.String() == "esc" {
			d.Hide()
		}
		return d, nil
	}

	switch keyMsg.String() {
	case "esc":
		d.Hide()
		return d, func() tea.Msg { return showWorkspacePickerMsg{} }
	case "tab", "down":
		d.focusIndex = (d.focusIndex + 1) % 2
		d.updateFocus()
		return d, nil
	case "shift+tab", "up":
		d.focusIndex = (d.focusIndex + 1) % 2
		d.updateFocus()
		return d, nil
	case "enter":
		name := strings.TrimSpace(d.nameInput.Value())
		if name == "" {
			d.err = "Name cannot be empty"
			return d, nil
		}
		d.err = ""
		branch := strings.TrimSpace(d.branchInput.Value())
		return d, func() tea.Msg { return workspaceCreateMsg{name: name, branch: branch} }
	}

	// Route to focused input.
	var cmd tea.Cmd
	if d.focusIndex == 0 {
		d.nameInput, cmd = d.nameInput.Update(msg)
	} else {
		d.branchInput, cmd = d.branchInput.Update(msg)
	}
	return d, cmd
}

// View renders the create workspace dialog.
func (d *CreateWorkspaceDialog) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("Create Workspace"))
	b.WriteString("\n\n")

	if d.creating {
		name := d.nameInput.Value()
		b.WriteString(lipgloss.NewStyle().Foreground(ColorText).Render("  Creating \"" + name + "\"..."))
		b.WriteString("\n")
		b.WriteString(DimStyle.Render("  Running provider command"))
		b.WriteString("\n\n")
		b.WriteString(DimStyle.Render("esc: cancel"))
		return d.wrapDialog(b.String())
	}

	b.WriteString(DimStyle.Render("Name:"))
	b.WriteString("\n")
	b.WriteString(d.nameInput.View())
	b.WriteString("\n\n")

	b.WriteString(DimStyle.Render("Branch (optional):"))
	b.WriteString("\n")
	b.WriteString(d.branchInput.View())
	b.WriteString("\n")

	if d.err != "" {
		b.WriteString("\n")
		b.WriteString(ErrorStyle.Render("  " + d.err))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(DimStyle.Render("enter: create  tab: next  esc: back"))

	return d.wrapDialog(b.String())
}

func (d *CreateWorkspaceDialog) wrapDialog(content string) string {
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
