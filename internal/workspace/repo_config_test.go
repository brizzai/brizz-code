package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	t.Run("no config files returns GitWorktreeProvider", func(t *testing.T) {
		repo := t.TempDir()
		got := ResolveProvider(repo)
		if _, ok := got.(*GitWorktreeProvider); !ok {
			t.Errorf("got %T, want *GitWorktreeProvider", got)
		}
	})

	t.Run(".fleet.json with shell commands wins", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".fleet.json", `{"workspace":{"create":"new-fleet","destroy":"destroy-fleet"}}`)
		got, ok := ResolveProvider(repo).(*ShellProvider)
		if !ok {
			t.Fatalf("got %T, want *ShellProvider", got)
		}
		if got.CreateCmd != "new-fleet" {
			t.Errorf("CreateCmd: got %q, want %q", got.CreateCmd, "new-fleet")
		}
	})

	t.Run("legacy .bc.json is used when .fleet.json absent", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".bc.json", `{"workspace":{"create":"new-bc"}}`)
		got, ok := ResolveProvider(repo).(*ShellProvider)
		if !ok {
			t.Fatalf("got %T, want *ShellProvider", got)
		}
		if got.CreateCmd != "new-bc" {
			t.Errorf("CreateCmd: got %q, want %q", got.CreateCmd, "new-bc")
		}
	})

	t.Run("empty .fleet.json suppresses .bc.json (presence wins)", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".fleet.json", `{}`)
		writeFile(t, repo, ".bc.json", `{"workspace":{"create":"new-bc"}}`)
		got := ResolveProvider(repo)
		if _, ok := got.(*GitWorktreeProvider); !ok {
			t.Errorf("got %T, want *GitWorktreeProvider — empty .fleet.json should suppress .bc.json", got)
		}
	})

	t.Run("malformed .fleet.json still suppresses .bc.json", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".fleet.json", `{not valid json`)
		writeFile(t, repo, ".bc.json", `{"workspace":{"create":"new-bc"}}`)
		got := ResolveProvider(repo)
		if _, ok := got.(*GitWorktreeProvider); !ok {
			t.Errorf("got %T, want *GitWorktreeProvider — malformed .fleet.json should still suppress .bc.json", got)
		}
	})

	t.Run("local override is field-by-field", func(t *testing.T) {
		repo := t.TempDir()
		writeFile(t, repo, ".fleet.json", `{"workspace":{"create":"base-create","destroy":"base-destroy"}}`)
		writeFile(t, repo, ".fleet.local.json", `{"workspace":{"create":"local-create"}}`)
		got, ok := ResolveProvider(repo).(*ShellProvider)
		if !ok {
			t.Fatalf("got %T, want *ShellProvider", got)
		}
		if got.CreateCmd != "local-create" {
			t.Errorf("CreateCmd: got %q, want %q (local should override)", got.CreateCmd, "local-create")
		}
		if got.DestroyCmd != "base-destroy" {
			t.Errorf("DestroyCmd: got %q, want %q (base should be preserved)", got.DestroyCmd, "base-destroy")
		}
	})
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
