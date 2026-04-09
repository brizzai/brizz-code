package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/brizzai/brizz-code/internal/debuglog"
)

// Golden file tests: real ANSI pane captures from tmux, tested through the
// full detection pipeline (StripANSI → extractRecentLines → detectStatus).
//
// To add a regression test when a bug is found:
//   1. tmux capture-pane -t <session> -p -e > internal/session/testdata/<name>.txt
//   2. Add an entry to goldenTests with the expected status
//   3. Run tests — should FAIL with the bug, PASS after fix

var goldenTests = []struct {
	fixture  string // filename in testdata/
	expected Status
	desc     string // what this fixture tests
}{
	{"pane_waiting_permission_3opt.txt", StatusWaiting, "3-option permission menu with Esc to cancel"},
	{"pane_finished_idle_prompt.txt", StatusFinished, "idle Claude prompt (❯)"},
	{"pane_finished_permission_mode.txt", StatusFinished, "permission mode bar (⏵⏵)"},
}

func TestGoldenDetection(t *testing.T) {
	debuglog.Init()

	for _, tt := range goldenTests {
		t.Run(tt.fixture, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", tt.fixture))
			if err != nil {
				t.Fatalf("failed to load fixture %s: %v", tt.fixture, err)
			}

			raw := string(data)
			stripped := StripANSI(raw)

			log := debuglog.Logger
			result := detectStatus(stripped, log)

			if result != tt.expected {
				t.Errorf("fixture %s (%s):\n  expected: %q\n  got:      %q", tt.fixture, tt.desc, tt.expected, result)

				// Dump debug info for diagnosis.
				lines := strings.Split(strings.TrimRight(stripped, "\n"), "\n")
				recent := extractRecentLines(lines, 50)
				limit := 10
				if limit > len(recent) {
					limit = len(recent)
				}
				t.Logf("bottom %d recent lines:", limit)
				for i := 0; i < limit; i++ {
					t.Logf("  [%d] %q", i, recent[i])
				}
			}
		})
	}
}

// TestGoldenANSIStripping verifies that StripANSI produces clean output
// from real ANSI captures (no escape sequences remain).
func TestGoldenANSIStripping(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Fatalf("failed to read testdata: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}
		t.Run(entry.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", entry.Name()))
			if err != nil {
				t.Fatalf("failed to load %s: %v", entry.Name(), err)
			}

			stripped := StripANSI(string(data))

			if strings.ContainsRune(stripped, '\x1b') {
				t.Errorf("stripped output still contains ESC (\\x1b)")
			}
			if strings.ContainsRune(stripped, '\x9B') {
				t.Errorf("stripped output still contains C1 CSI (\\x9B)")
			}
		})
	}
}
