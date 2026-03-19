package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ReadClaudeSessionName reads the custom session name from Claude's JSONL conversation file.
// Returns empty string if not found.
func ReadClaudeSessionName(claudeSessionID, projectPath string) string {
	if claudeSessionID == "" || projectPath == "" {
		return ""
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	// Convert project path to Claude's dir format: /home/user/code/foo → -home-user-code-foo
	dirName := strings.ReplaceAll(projectPath, "/", "-")

	jsonlPath := filepath.Join(homeDir, ".claude", "projects", dirName, claudeSessionID+".jsonl")

	f, err := os.Open(jsonlPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max buffer

	var lastTitle string
	for scanner.Scan() {
		line := scanner.Text()
		// Quick check before JSON parsing.
		if !strings.Contains(line, "custom-title") {
			continue
		}

		var entry struct {
			Type        string `json:"type"`
			CustomTitle string `json:"customTitle"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "custom-title" && entry.CustomTitle != "" {
			lastTitle = entry.CustomTitle
		}
	}

	return lastTitle
}
