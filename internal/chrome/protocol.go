package chrome

import (
	"os"
	"path/filepath"
)

// Action constants for the command protocol.
const (
	ActionOpenOrFocus  = "open_or_focus"
	ActionCloseTab     = "close_tab"
	ActionCreateGroup  = "create_tab_group"
	ActionPing         = "ping"
)

// Command represents a request sent to the Chrome extension.
type Command struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
	Group  string `json:"group,omitempty"`
	Name   string `json:"name,omitempty"`
	Color  string `json:"color,omitempty"`
}

// Response represents a response from the Chrome extension.
type Response struct {
	ID      string                 `json:"id"`
	Success bool                   `json:"success"`
	Error   string                 `json:"error,omitempty"`
	Data    map[string]any `json:"data,omitempty"`
}

// SocketPath returns the Unix socket path for the chrome bridge.
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "brizz-code", "chrome.sock")
}
