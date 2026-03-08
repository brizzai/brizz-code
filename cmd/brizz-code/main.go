package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/yuvalhayke/brizz-code/internal/debuglog"
	"github.com/yuvalhayke/brizz-code/internal/session"
	"github.com/yuvalhayke/brizz-code/internal/tmux"
	"github.com/yuvalhayke/brizz-code/internal/ui"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		runTUI()
		return
	}

	switch args[0] {
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: brizz-code add <path>")
			os.Exit(1)
		}
		runAdd(args[1])
	case "list", "ls":
		runList()
	case "remove", "rm":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: brizz-code remove <id>")
			os.Exit(1)
		}
		runRemove(args[1])
	case "hook-handler":
		handleHookHandler()
	case "hooks":
		handleHooksCmd(args[1:])
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func runTUI() {
	debuglog.Init()
	defer debuglog.Close()
	debuglog.Logger.Info("brizz-code TUI starting")

	if err := tmux.IsTmuxAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	storage, err := session.Open(session.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	model := ui.NewHome(storage)
	p := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runAdd(path string) {
	if err := tmux.IsTmuxAvailable(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Expand and validate path.
	path = expandPath(path)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "Invalid directory: %s\n", path)
		os.Exit(1)
	}

	storage, err := session.Open(session.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	title := session.TitleFromPath(path)
	s := session.NewSession(title, path)

	if err := s.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start session: %v\n", err)
		os.Exit(1)
	}

	if err := storage.SaveSession(s.ToRow()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to save session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created session '%s' (%s)\n", title, s.ID)
}

func runList() {
	storage, err := session.Open(session.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	rows, err := storage.LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load sessions: %v\n", err)
		os.Exit(1)
	}

	if len(rows) == 0 {
		fmt.Println("No sessions.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTITLE\tSTATUS\tPATH")
	for _, r := range rows {
		// Show short ID.
		shortID := r.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", shortID, r.Title, r.Status, r.ProjectPath)
	}
	w.Flush()
}

func runRemove(idPrefix string) {
	storage, err := session.Open(session.DefaultDBPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
		os.Exit(1)
	}
	defer storage.Close()

	rows, err := storage.LoadSessions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load sessions: %v\n", err)
		os.Exit(1)
	}

	// Find session by ID prefix.
	var match *session.SessionRow
	for _, r := range rows {
		if strings.HasPrefix(r.ID, idPrefix) {
			if match != nil {
				fmt.Fprintln(os.Stderr, "Ambiguous ID prefix, be more specific")
				os.Exit(1)
			}
			match = r
		}
	}

	if match == nil {
		fmt.Fprintf(os.Stderr, "No session found with ID starting with '%s'\n", idPrefix)
		os.Exit(1)
	}

	// Kill tmux session if alive.
	ts := tmux.ReconnectSession(match.TmuxSession, match.Title, match.ProjectPath)
	if ts.Exists() {
		_ = ts.Kill()
	}

	if err := storage.DeleteSession(match.ID); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to delete session: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Removed session '%s' (%s)\n", match.Title, match.ID)
}

func printUsage() {
	fmt.Println(`brizz-code - manage Claude Code sessions

Usage:
  brizz-code              Launch TUI
  brizz-code add <path>   Add a new session
  brizz-code list         List all sessions
  brizz-code remove <id>  Remove a session
  brizz-code hooks <install|uninstall|status>  Manage Claude Code hooks
  brizz-code help         Show this help`)
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}
