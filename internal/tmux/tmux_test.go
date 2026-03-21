package tmux

import "testing"

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"alphanumeric", "hello123", "hello123"},
		{"with hyphens", "my-session", "my-session"},
		{"with spaces", "my session", "my-session"},
		{"with special chars", "hello@world#123", "hello-world-123"},
		{"leading hyphens", "---hello", "hello"},
		{"trailing hyphens", "hello---", "hello"},
		{"consecutive hyphens collapsed", "hello---world", "hello-world"},
		{"empty string", "", "session"},
		{"all special chars", "@#$%^&", "session"},
		{"uppercase preserved", "MySession", "MySession"},
		{"mixed", "My Cool Session! (v2)", "My-Cool-Session-v2"},
		{"long name truncated", "abcdefghijklmnopqrstuvwxyz12345678901234567890", "abcdefghijklmnopqrstuvwxyz1234"},
		{"exactly 30", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"dots replaced", "my.session.name", "my-session-name"},
		{"underscores replaced", "my_session_name", "my-session-name"},
		{"slash replaced", "path/to/session", "path-to-session"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeName(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNewSessionNameFormat(t *testing.T) {
	s := NewSession("test-session", "/tmp/workdir")

	// Name should start with the prefix.
	if len(s.Name) < len(SessionPrefix) {
		t.Fatalf("session name too short: %q", s.Name)
	}
	prefix := s.Name[:len(SessionPrefix)]
	if prefix != SessionPrefix {
		t.Errorf("session name prefix: got %q, want %q", prefix, SessionPrefix)
	}

	// DisplayName and WorkDir should be preserved.
	if s.DisplayName != "test-session" {
		t.Errorf("DisplayName: got %q, want %q", s.DisplayName, "test-session")
	}
	if s.WorkDir != "/tmp/workdir" {
		t.Errorf("WorkDir: got %q, want %q", s.WorkDir, "/tmp/workdir")
	}
}

func TestReconnectSession(t *testing.T) {
	s := ReconnectSession("brizzcode_test_abc123", "My Session", "/home/user/project")

	if s.Name != "brizzcode_test_abc123" {
		t.Errorf("Name: got %q, want %q", s.Name, "brizzcode_test_abc123")
	}
	if s.DisplayName != "My Session" {
		t.Errorf("DisplayName: got %q, want %q", s.DisplayName, "My Session")
	}
	if s.WorkDir != "/home/user/project" {
		t.Errorf("WorkDir: got %q, want %q", s.WorkDir, "/home/user/project")
	}
}

func TestGenerateShortIDUniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := generateShortID()
		if len(id) != 8 {
			t.Errorf("generateShortID() returned %q (len %d), expected 8 hex chars", id, len(id))
		}
		if seen[id] {
			t.Errorf("generateShortID() produced duplicate: %q", id)
		}
		seen[id] = true
	}
}
