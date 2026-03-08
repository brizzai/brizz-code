package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpOverlay shows a keybindings cheat sheet.
type HelpOverlay struct {
	visible bool
	width   int
	height  int
}

// NewHelpOverlay creates a new help overlay.
func NewHelpOverlay() *HelpOverlay {
	return &HelpOverlay{}
}

func (h *HelpOverlay) Show()              { h.visible = true }
func (h *HelpOverlay) Hide()              { h.visible = false }
func (h *HelpOverlay) IsVisible() bool    { return h.visible }
func (h *HelpOverlay) SetSize(w, ht int)  { h.width = w; h.height = ht }

// Update dismisses on any key press.
func (h *HelpOverlay) Update(msg tea.Msg) (*HelpOverlay, tea.Cmd) {
	if _, ok := msg.(tea.KeyMsg); ok {
		h.Hide()
		return h, nil
	}
	return h, nil
}

// View renders the keybinding cheat sheet.
func (h *HelpOverlay) View() string {
	bindings := []struct{ key, desc string }{
		{"j / ↓", "Move down"},
		{"k / ↑", "Move up"},
		{"Enter", "Attach / toggle group"},
		{"a / n", "New session"},
		{"d", "Delete session"},
		{"r", "Restart session"},
		{"q", "Quit"},
		{"?", "Toggle help"},
		{"", ""},
		{"Ctrl+Q", "Detach from session"},
	}

	var b strings.Builder
	b.WriteString(TitleStyle.Render("Keybindings"))
	b.WriteString("\n\n")

	for _, bind := range bindings {
		if bind.key == "" {
			b.WriteString("\n")
			continue
		}
		key := lipgloss.NewStyle().
			Foreground(ColorAccent).
			Bold(true).
			Width(12).
			Render(bind.key)
		b.WriteString("  " + key + "  " + bind.desc + "\n")
	}

	b.WriteString("\n")
	b.WriteString(DimStyle.Render("Press any key to close"))

	box := DialogStyle.Width(40).Render(b.String())
	return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, box)
}
