package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// commandPaletteMsg is sent when the user selects a command.
type commandPaletteMsg struct {
	commandID string
}

// PaletteCommand represents a single action in the command palette.
type PaletteCommand struct {
	ID       string // e.g. "restart", "reload_all"
	Name     string // e.g. "Restart Session"
	Shortcut string // e.g. "r" (empty for palette-only commands)
}

// CommandPaletteDialog shows a fuzzy-filterable list of available commands.
type CommandPaletteDialog struct {
	visible       bool
	width, height int
	commands      []PaletteCommand // full list
	filtered      []scoredCommand  // after fuzzy filter
	cursor        int
	scrollOff     int
	filterInput   textinput.Model
}

type scoredCommand struct {
	PaletteCommand
	score int
}

const paletteMaxVisible = 14

// NewCommandPaletteDialog creates a new command palette dialog.
func NewCommandPaletteDialog() *CommandPaletteDialog {
	fi := textinput.New()
	fi.Placeholder = "type a command..."
	fi.CharLimit = 64
	fi.Width = 40

	return &CommandPaletteDialog{
		filterInput: fi,
	}
}

// Show populates and opens the palette.
func (d *CommandPaletteDialog) Show(commands []PaletteCommand) {
	d.visible = true
	d.commands = commands
	d.cursor = 0
	d.scrollOff = 0
	d.filterInput.SetValue("")
	d.filterInput.Focus()
	d.rebuildFiltered()
}

// Hide closes the palette.
func (d *CommandPaletteDialog) Hide() {
	d.visible = false
	d.filterInput.Blur()
}

// IsVisible returns whether the palette is shown.
func (d *CommandPaletteDialog) IsVisible() bool { return d.visible }

// SetSize updates dimensions.
func (d *CommandPaletteDialog) SetSize(w, h int) {
	d.width = w
	d.height = h
}

func (d *CommandPaletteDialog) rebuildFiltered() {
	query := strings.TrimSpace(d.filterInput.Value())
	d.filtered = nil

	for _, cmd := range d.commands {
		if query == "" {
			d.filtered = append(d.filtered, scoredCommand{PaletteCommand: cmd, score: 0})
			continue
		}
		matched, score := fuzzyMatch(query, cmd.Name)
		if matched {
			d.filtered = append(d.filtered, scoredCommand{PaletteCommand: cmd, score: score})
		}
	}

	// Sort by score descending when filtering.
	if query != "" {
		sort.SliceStable(d.filtered, func(i, j int) bool {
			return d.filtered[i].score > d.filtered[j].score
		})
	}

	// Clamp cursor.
	if d.cursor >= len(d.filtered) {
		d.cursor = len(d.filtered) - 1
	}
	if d.cursor < 0 {
		d.cursor = 0
	}
	d.syncScroll()
}

func (d *CommandPaletteDialog) syncScroll() {
	if len(d.filtered) <= paletteMaxVisible {
		d.scrollOff = 0
		return
	}
	if d.cursor < d.scrollOff {
		d.scrollOff = d.cursor
	}
	if d.cursor >= d.scrollOff+paletteMaxVisible {
		d.scrollOff = d.cursor - paletteMaxVisible + 1
	}
}

// Update handles key events.
func (d *CommandPaletteDialog) Update(msg tea.Msg) (*CommandPaletteDialog, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return d, nil
	}

	switch keyMsg.String() {
	case "esc":
		d.Hide()
		return d, nil
	case "up", "ctrl+k":
		if d.cursor > 0 {
			d.cursor--
			d.syncScroll()
		}
		return d, nil
	case "down", "ctrl+j":
		if d.cursor < len(d.filtered)-1 {
			d.cursor++
			d.syncScroll()
		}
		return d, nil
	case "enter":
		if d.cursor < 0 || d.cursor >= len(d.filtered) {
			return d, nil
		}
		selected := d.filtered[d.cursor]
		d.Hide()
		return d, func() tea.Msg {
			return commandPaletteMsg{commandID: selected.ID}
		}
	}

	// Route all other keys to the text input.
	var cmd tea.Cmd
	d.filterInput, cmd = d.filterInput.Update(msg)
	d.rebuildFiltered()
	return d, cmd
}

// View renders the command palette dialog.
func (d *CommandPaletteDialog) View() string {
	var b strings.Builder

	b.WriteString(TitleStyle.Render("Command Palette"))
	b.WriteString("\n\n")

	// Search input.
	b.WriteString("  " + DimStyle.Render(">") + " " + d.filterInput.View())
	b.WriteString("\n\n")

	if len(d.filtered) == 0 {
		b.WriteString(DimStyle.Render("  No matching commands"))
		b.WriteString("\n")
	} else {
		// Scroll indicators.
		if d.scrollOff > 0 {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ⋮ +%d above", d.scrollOff)))
			b.WriteString("\n")
		}

		end := d.scrollOff + paletteMaxVisible
		if end > len(d.filtered) {
			end = len(d.filtered)
		}

		// Calculate max name width for shortcut alignment.
		maxName := 0
		for i := d.scrollOff; i < end; i++ {
			if len(d.filtered[i].Name) > maxName {
				maxName = len(d.filtered[i].Name)
			}
		}

		for i := d.scrollOff; i < end; i++ {
			cmd := d.filtered[i]
			selected := i == d.cursor

			prefix := "  "
			if selected {
				prefix = SessionSelectionPrefix.Render("▸ ")
			}

			name := cmd.Name
			// Pad name for shortcut alignment.
			padded := name + strings.Repeat(" ", maxName-len(name)+2)

			if selected {
				b.WriteString(prefix + SessionTitleSelStyle.Render(name))
				if cmd.Shortcut != "" {
					b.WriteString(strings.Repeat(" ", maxName-len(name)+2) + DimStyle.Render(cmd.Shortcut))
				}
			} else {
				b.WriteString(prefix + lipgloss.NewStyle().Foreground(ColorText).Render(padded))
				if cmd.Shortcut != "" {
					b.WriteString(DimStyle.Render(cmd.Shortcut))
				}
			}
			b.WriteString("\n")
		}

		below := len(d.filtered) - end
		if below > 0 {
			b.WriteString(DimStyle.Render(fmt.Sprintf("  ⋮ +%d below", below)))
			b.WriteString("\n")
		}
	}

	b.WriteString("\n")
	b.WriteString(DimStyle.Render("↑↓ nav  enter: run  esc: close"))

	return d.wrapDialog(b.String())
}

func (d *CommandPaletteDialog) wrapDialog(content string) string {
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

// fuzzyMatch checks if all characters in query appear in target (in order)
// and returns a relevance score.
func fuzzyMatch(query, target string) (bool, int) {
	q := strings.ToLower(query)
	t := strings.ToLower(target)

	qi := 0
	score := 0
	prevMatch := false

	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if t[ti] == q[qi] {
			qi++
			score++
			if prevMatch {
				score += 2 // consecutive match bonus
			}
			if ti == 0 || t[ti-1] == ' ' {
				score += 3 // word boundary bonus
			}
			prevMatch = true
		} else {
			prevMatch = false
		}
	}

	return qi == len(q), score
}
