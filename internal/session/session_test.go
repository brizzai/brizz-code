package session

import (
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestStripANSI(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no ansi", "hello world", "hello world"},
		{"CSI color", "\x1b[31mred\x1b[0m", "red"},
		{"CSI bold+color", "\x1b[1;32mbold green\x1b[0m", "bold green"},
		{"OSC hyperlink", "\x1b]8;;https://example.com\x07link\x1b]8;;\x07", "link"},
		{"OSC with ST", "\x1b]8;;https://example.com\x1b\\link\x1b]8;;\x1b\\", "link"},
		// C1 CSI with ESC prefix so fast path doesn't skip (raw 0x9B byte isn't found by ContainsRune).
		{"C1 CSI with ESC", "\x1b[0m\x9B31mred\x9B0m", "red"},
		{"mixed", "\x1b[1mhello\x1b[0m \x1b]8;;url\x07world\x1b]8;;\x07", "hello world"},
		{"empty", "", ""},
		{"no escape fast path", "plain text 123", "plain text 123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSI(tt.input)
			if got != tt.want {
				t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestDetectStatus(t *testing.T) {
	log := slog.Default()

	tests := []struct {
		name    string
		content string
		want    Status
	}{
		{"empty", "", ""},
		{"busy pattern", "some output\nctrl+c to interrupt\n", StatusRunning},
		{"esc busy pattern", "output\nesc to interrupt\n", StatusRunning},
		{"spinner char", "⠋ Working...\n", StatusRunning},
		{"whimsical pattern", "Clauding… (53s · ↓ 749 tokens)\n", StatusRunning},
		{"approval yes allow", "some text\nYes, allow once\n", StatusWaiting},
		{"approval continue", "Continue?\n", StatusWaiting},
		{"approval yn", "(Y/n)\n", StatusWaiting},
		{"prompt indicator >", "output\n>\n", StatusFinished},
		{"prompt indicator ❯", "❯\n", StatusFinished},
		{"prompt with space", "> \n", StatusFinished},
		{"idle pattern", "⏵⏵\n", StatusFinished},
		{"no match", "random output text\nmore text\n", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectStatus(tt.content, log)
			if got != tt.want {
				t.Errorf("detectStatus(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestApplyHookWaiting_PaneOverride(t *testing.T) {
	log := slog.Default()

	tests := []struct {
		name           string
		acknowledged   bool
		paneStatus     Status
		wantStatus     Status
		wantHashReset  bool
		wantEarlyExit  bool // should return before content hash tracking
	}{
		{
			name:          "pane finished overrides to finished",
			acknowledged:  false,
			paneStatus:    StatusFinished,
			wantStatus:    StatusFinished,
			wantHashReset: true,
			wantEarlyExit: true,
		},
		{
			name:          "pane finished with acknowledged overrides to idle",
			acknowledged:  true,
			paneStatus:    StatusFinished,
			wantStatus:    StatusIdle,
			wantHashReset: true,
			wantEarlyExit: true,
		},
		{
			name:         "pane waiting keeps waiting",
			acknowledged: false,
			paneStatus:   StatusWaiting,
			wantStatus:   StatusWaiting,
		},
		{
			name:         "pane running keeps waiting",
			acknowledged: false,
			paneStatus:   StatusRunning,
			wantStatus:   StatusWaiting,
		},
		{
			name:         "empty pane status keeps waiting",
			acknowledged: false,
			paneStatus:   "",
			wantStatus:   StatusWaiting,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{
				Status:              StatusWaiting,
				Acknowledged:        tt.acknowledged,
				lastContentHash:     "somehash",
				lastContentChangeAt: time.Now(),
			}

			// Use empty pane content for non-override cases to avoid
			// content change detection interfering with the test.
			paneContent := ""
			if tt.wantHashReset {
				paneContent = "some pane content"
			}

			s.mu.Lock()
			s.applyHookWaiting(paneContent, tt.paneStatus, log)
			s.mu.Unlock()

			if s.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", s.Status, tt.wantStatus)
			}
			if tt.wantHashReset {
				if s.lastContentHash != "" {
					t.Errorf("lastContentHash should be reset, got %q", s.lastContentHash)
				}
				if !s.lastContentChangeAt.IsZero() {
					t.Errorf("lastContentChangeAt should be zero, got %v", s.lastContentChangeAt)
				}
			}
		})
	}
}

func TestTitleFromPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"absolute", "/Users/test/code/myproject", "myproject"},
		{"relative", "code/myproject", "myproject"},
		{"trailing slash", "/Users/test/code/myproject/", "myproject"},
		{"single segment", "myproject", "myproject"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TitleFromPath(tt.path)
			if got != tt.want {
				t.Errorf("TitleFromPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestGenerateID(t *testing.T) {
	id := generateID()

	// Format: <8hex>-<unix_timestamp>
	matched, err := regexp.MatchString(`^[0-9a-f]{8}-\d+$`, id)
	if err != nil {
		t.Fatalf("regex error: %v", err)
	}
	if !matched {
		t.Errorf("generateID() = %q, does not match expected format <8hex>-<timestamp>", id)
	}

	// Uniqueness check.
	id2 := generateID()
	if id == id2 {
		t.Errorf("generateID() produced duplicate IDs: %q", id)
	}
}

func TestHashContent(t *testing.T) {
	h1 := hashContent("hello")
	h2 := hashContent("hello")
	h3 := hashContent("world")

	if h1 != h2 {
		t.Errorf("hashContent should be deterministic: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("hashContent should differ for different inputs")
	}
	if len(h1) != 16 {
		t.Errorf("hashContent should return 16 hex chars, got %d", len(h1))
	}
}

func TestNormalizeForHash(t *testing.T) {
	// Should strip spinner chars.
	input := "⠋ Working on task\n\n\n\nDone"
	result := normalizeForHash(input)
	if strings.Contains(result, "⠋") {
		t.Errorf("normalizeForHash should strip spinner chars")
	}

	// Should collapse consecutive blank lines (3+ newlines -> 2 newlines).
	if strings.Contains(result, "\n\n\n") {
		t.Errorf("normalizeForHash should collapse consecutive blank lines, got: %q", result)
	}
}
