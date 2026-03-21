package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadClaudeSessionName(t *testing.T) {
	t.Run("empty claudeSessionID returns empty", func(t *testing.T) {
		got := ReadClaudeSessionName("", "/some/path")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("empty projectPath returns empty", func(t *testing.T) {
		got := ReadClaudeSessionName("some-id", "")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("both empty returns empty", func(t *testing.T) {
		got := ReadClaudeSessionName("", "")
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		got := ReadClaudeSessionName("nonexistent-id", "/nonexistent/path")
		if got != "" {
			t.Errorf("expected empty for missing file, got %q", got)
		}
	})

	t.Run("valid JSONL with custom-title returns last title", func(t *testing.T) {
		// Set up a temp dir structure mimicking ~/.claude/projects/<dir>/<id>.jsonl
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home directory")
		}

		projectPath := "/test/my-project"
		claudeSessionID := "test-session-abc123"
		dirName := "-test-my-project"

		projectDir := filepath.Join(homeDir, ".claude", "projects", dirName)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}
		t.Cleanup(func() {
			os.RemoveAll(projectDir)
		})

		jsonlPath := filepath.Join(projectDir, claudeSessionID+".jsonl")
		content := `{"type":"message","content":"hello"}
{"type":"custom-title","customTitle":"First Title"}
{"type":"message","content":"world"}
{"type":"custom-title","customTitle":"Second Title"}
{"type":"message","content":"done"}
`
		if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write JSONL: %v", err)
		}

		got := ReadClaudeSessionName(claudeSessionID, projectPath)
		if got != "Second Title" {
			t.Errorf("expected %q, got %q", "Second Title", got)
		}
	})

	t.Run("JSONL with no custom-title entries returns empty", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home directory")
		}

		projectPath := "/test/no-titles"
		claudeSessionID := "test-session-notitles"
		dirName := "-test-no-titles"

		projectDir := filepath.Join(homeDir, ".claude", "projects", dirName)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}
		t.Cleanup(func() {
			os.RemoveAll(projectDir)
		})

		jsonlPath := filepath.Join(projectDir, claudeSessionID+".jsonl")
		content := `{"type":"message","content":"hello"}
{"type":"message","content":"world"}
`
		if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write JSONL: %v", err)
		}

		got := ReadClaudeSessionName(claudeSessionID, projectPath)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("JSONL with custom-title but empty customTitle is skipped", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home directory")
		}

		projectPath := "/test/empty-title"
		claudeSessionID := "test-session-emptytitle"
		dirName := "-test-empty-title"

		projectDir := filepath.Join(homeDir, ".claude", "projects", dirName)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}
		t.Cleanup(func() {
			os.RemoveAll(projectDir)
		})

		jsonlPath := filepath.Join(projectDir, claudeSessionID+".jsonl")
		content := `{"type":"custom-title","customTitle":""}
{"type":"message","content":"hello"}
`
		if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write JSONL: %v", err)
		}

		got := ReadClaudeSessionName(claudeSessionID, projectPath)
		if got != "" {
			t.Errorf("expected empty for empty customTitle, got %q", got)
		}
	})

	t.Run("JSONL with malformed JSON lines are skipped", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Skip("cannot determine home directory")
		}

		projectPath := "/test/malformed"
		claudeSessionID := "test-session-malformed"
		dirName := "-test-malformed"

		projectDir := filepath.Join(homeDir, ".claude", "projects", dirName)
		if err := os.MkdirAll(projectDir, 0755); err != nil {
			t.Fatalf("failed to create project dir: %v", err)
		}
		t.Cleanup(func() {
			os.RemoveAll(projectDir)
		})

		jsonlPath := filepath.Join(projectDir, claudeSessionID+".jsonl")
		content := `not valid json custom-title
{"type":"custom-title","customTitle":"Valid Title"}
{broken json custom-title
`
		if err := os.WriteFile(jsonlPath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to write JSONL: %v", err)
		}

		got := ReadClaudeSessionName(claudeSessionID, projectPath)
		if got != "Valid Title" {
			t.Errorf("expected %q, got %q", "Valid Title", got)
		}
	})
}
