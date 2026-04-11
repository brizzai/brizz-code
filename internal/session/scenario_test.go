package session

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/brizzai/brizz-code/internal/debuglog"
)

// --- Mock pane capturer ---

type mockPane struct {
	content string
	dead    bool
	alive   bool // controls IsAlive via !IsPaneDead
}

func (m *mockPane) CapturePane() (string, error) { return m.content, nil }
func (m *mockPane) IsPaneDead() bool             { return m.dead }

// --- Scenario types ---

// ScenarioEvent describes something that happens at a point in time.
type ScenarioEvent struct {
	At          time.Duration // relative to scenario start
	Hook        string        // hook status: "running", "waiting", "finished", "dead", or "" (no change)
	Pane        string        // pane content to set (raw string or "@fixture:filename.txt" for golden file)
	PaneDead    bool          // simulate pane death
	Acknowledge bool          // simulate user acknowledging the session
}

// ScenarioCheck asserts the session status at a point in time.
type ScenarioCheck struct {
	At       time.Duration
	Expected Status
}

// Scenario is a named sequence of events and status checks.
type Scenario struct {
	Name   string
	Events []ScenarioEvent
	Checks []ScenarioCheck
}

// --- Replay engine ---

// timelineEntry merges events and checks into a single sorted timeline.
type timelineEntry struct {
	at    time.Duration
	event *ScenarioEvent
	check *ScenarioCheck
}

func runScenario(t *testing.T, sc Scenario) {
	t.Helper()
	debuglog.Init()

	mock := &mockPane{alive: true}
	s := &Session{
		ID:           "test-scenario",
		Title:        sc.Name,
		Status:       StatusStarting,
		paneCapturer: mock,
	}

	// Build sorted timeline.
	var timeline []timelineEntry
	for i := range sc.Events {
		e := sc.Events[i]
		timeline = append(timeline, timelineEntry{at: e.At, event: &e})
	}
	for i := range sc.Checks {
		c := sc.Checks[i]
		timeline = append(timeline, timelineEntry{at: c.At, check: &c})
	}
	sort.SliceStable(timeline, func(i, j int) bool {
		if timeline[i].at == timeline[j].at {
			// Events before checks at the same timestamp.
			return timeline[i].event != nil && timeline[j].check != nil
		}
		return timeline[i].at < timeline[j].at
	})

	currentHook := ""
	var hookUpdatedAt time.Time

	for _, entry := range timeline {
		if entry.event != nil {
			e := entry.event

			// Apply hook status change.
			if e.Hook != "" {
				currentHook = e.Hook
				hookUpdatedAt = time.Now()
				s.UpdateHookStatus(&HookStatus{
					Status:    e.Hook,
					UpdatedAt: hookUpdatedAt,
				})
			}

			// Set pane content.
			if e.Pane != "" {
				mock.content = loadPaneContent(t, e.Pane)
			}

			// Set pane dead state.
			mock.dead = e.PaneDead

			// Acknowledge.
			if e.Acknowledge {
				s.mu.Lock()
				s.Acknowledged = true
				s.mu.Unlock()
			}

			// Adjust content timing for stability checks.
			// If this event sets the same pane content as before, we need to
			// simulate time passing. We do this by adjusting lastContentChangeAt
			// backwards by the delta between this event and the previous one.
			if e.At > 0 {
				s.mu.Lock()
				if s.lastContentChangeAt.IsZero() {
					s.lastContentChangeAt = time.Now().Add(-e.At)
				}
				s.mu.Unlock()
			}
		}

		if entry.check != nil {
			c := entry.check

			// For time-based checks (content stability), we need to fake the
			// elapsed time. Set lastContentChangeAt to simulate the right age.
			if c.At > 0 {
				s.mu.Lock()
				if !s.lastContentChangeAt.IsZero() {
					// Content has been stable since it was set. Adjust so
					// time.Since(lastContentChangeAt) reflects the scenario time.
					s.lastContentChangeAt = time.Now().Add(-c.At)
				}
				s.mu.Unlock()
			}

			// Run UpdateStatus to get the current classification.
			s.UpdateStatus()

			got := s.GetStatus()
			if got != c.Expected {
				t.Errorf("at t=%v: expected %q, got %q (hook=%q, paneDead=%v)",
					c.At, c.Expected, got, currentHook, mock.dead)
			}
		}
	}
}

// loadPaneContent loads pane content from a string or golden fixture.
// If content starts with "@fixture:", loads from testdata/.
func loadPaneContent(t *testing.T, content string) string {
	t.Helper()
	const prefix = "@fixture:"
	if len(content) > len(prefix) && content[:len(prefix)] == prefix {
		filename := content[len(prefix):]
		data, err := os.ReadFile(filepath.Join("testdata", filename))
		if err != nil {
			t.Fatalf("failed to load fixture %s: %v", filename, err)
		}
		return string(data)
	}
	return content
}

// --- Scenarios ---

func TestScenarioHappyPath(t *testing.T) {
	runScenario(t, Scenario{
		Name: "running → waiting → approved → running → finished",
		Events: []ScenarioEvent{
			{At: 0, Hook: "running", Pane: "⠋ Working on your request...\nctrl+c to interrupt\n"},
			{At: 3 * time.Second, Hook: "waiting", Pane: "output\n❯ 1. Yes\n  2. No\nEsc to cancel\n"},
			{At: 5 * time.Second, Hook: "running", Pane: "⠋ Applying changes...\nctrl+c to interrupt\n"},
			{At: 8 * time.Second, Hook: "finished", Pane: "Done!\n\n❯ \n"},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusRunning},
			{At: 3 * time.Second, Expected: StatusWaiting},
			{At: 5 * time.Second, Expected: StatusRunning},
			{At: 8 * time.Second, Expected: StatusFinished},
		},
	})
}

func TestScenarioUserEscapesPermission(t *testing.T) {
	runScenario(t, Scenario{
		Name: "waiting → user escapes → pane shows idle → finished",
		Events: []ScenarioEvent{
			{At: 0, Hook: "waiting", Pane: "output\n❯ 1. Yes\n  2. No\nEsc to cancel\n"},
			{At: 3 * time.Second, Pane: "❯ \n"}, // user pressed Escape, no hook fires
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusWaiting},
			{At: 3 * time.Second, Expected: StatusFinished},
		},
	})
}

func TestScenarioContentStabilityTimeout(t *testing.T) {
	stableContent := "Some output text\nMore output\n"
	runScenario(t, Scenario{
		Name: "running → content stable >10s → finished",
		Events: []ScenarioEvent{
			{At: 0, Hook: "running", Pane: stableContent},
		},
		Checks: []ScenarioCheck{
			{At: 5 * time.Second, Expected: StatusRunning},
			{At: 11 * time.Second, Expected: StatusFinished},
		},
	})
}

func TestScenarioSubAgentPermission(t *testing.T) {
	runScenario(t, Scenario{
		Name: "hook=finished but pane shows waiting → override to waiting",
		Events: []ScenarioEvent{
			{At: 0, Hook: "finished", Pane: "output\n❯ 1. Yes\n  2. No\nEsc to cancel\n"},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusWaiting},
		},
	})
}

func TestScenarioAcknowledgedPreventsOscillation(t *testing.T) {
	runScenario(t, Scenario{
		Name: "acknowledged idle stays idle when hook says waiting + pane idle",
		Events: []ScenarioEvent{
			{At: 0, Hook: "finished", Pane: "❯ \n", Acknowledge: true},
			{At: 2 * time.Second, Hook: "waiting", Pane: "❯ \n"}, // stale hook
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusIdle},
			{At: 2 * time.Second, Expected: StatusIdle},
		},
	})
}

func TestScenarioPaneDeath(t *testing.T) {
	runScenario(t, Scenario{
		Name: "session crash → error",
		Events: []ScenarioEvent{
			{At: 0, Hook: "running", Pane: "⠋ Working...\nctrl+c to interrupt\n"},
			{At: 2 * time.Second, PaneDead: true},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusRunning},
			{At: 2 * time.Second, Expected: StatusError},
		},
	})
}

func TestScenarioNoHooksFallback(t *testing.T) {
	runScenario(t, Scenario{
		Name: "no hooks, pane-only detection",
		Events: []ScenarioEvent{
			{At: 0, Pane: "⠋ Working...\nctrl+c to interrupt\n"},
			{At: 2 * time.Second, Pane: "output\n❯ 1. Yes\n  2. No\nEsc to cancel\n"},
			{At: 4 * time.Second, Pane: "Done!\n\n❯ \n"},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusRunning},
			{At: 2 * time.Second, Expected: StatusWaiting},
			{At: 4 * time.Second, Expected: StatusFinished},
		},
	})
}

func TestScenarioRealANSIPermission(t *testing.T) {
	// Uses the golden fixture from the real bug we found.
	fixture := "pane_waiting_permission_3opt.txt"
	if _, err := os.Stat(filepath.Join("testdata", fixture)); err != nil {
		t.Skipf("fixture %s not available", fixture)
	}

	runScenario(t, Scenario{
		Name: "real ANSI permission prompt: hook=waiting → should stay waiting",
		Events: []ScenarioEvent{
			{At: 0, Hook: "waiting", Pane: "@fixture:" + fixture},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusWaiting},
		},
	})
}

func TestScenarioStaleWaitingHookStaysFinished(t *testing.T) {
	// Regression: hook says "waiting" but user already rejected the permission.
	// The pane override correctly detects idle and sets finished, but on subsequent
	// cycles the stale hook used to flip the status back to waiting/running.
	// The fix records hookOverriddenAt to prevent re-evaluation of the same stale hook.
	idlePane := "Done!\n\n❯ \n"
	slightlyDifferentPane := "Done!\n\n❯ \nstatus bar updated\n" // simulates tmux status bar change

	runScenario(t, Scenario{
		Name: "stale waiting hook: override stays sticky across cycles",
		Events: []ScenarioEvent{
			{At: 0, Hook: "waiting", Pane: idlePane},                                            // first cycle: override to finished
			{At: 2 * time.Second, Pane: slightlyDifferentPane},                                  // pane changes slightly (status bar)
			{At: 4 * time.Second, Pane: idlePane},                                               // back to original
			{At: 6 * time.Second, Hook: "running", Pane: "⠋ Working...\nctrl+c to interrupt\n"}, // NEW hook resets flag
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusFinished},               // override fires
			{At: 2 * time.Second, Expected: StatusFinished}, // stays finished (stale hook, skip)
			{At: 4 * time.Second, Expected: StatusFinished}, // still finished
			{At: 6 * time.Second, Expected: StatusRunning},  // new hook, fresh evaluation
		},
	})
}

func TestScenarioWaitingRunningCooldown(t *testing.T) {
	// Regression: when hook says "waiting" but Claude is actively working
	// (e.g. AskUserQuestion response doesn't fire UserPromptSubmit), content
	// changes in bursts. Between bursts the hash is the same for a tick,
	// causing oscillation back to waiting. The 15s cooldown prevents this.
	runScenario(t, Scenario{
		Name: "waiting → content changes → stays running during cooldown",
		Events: []ScenarioEvent{
			{At: 0, Hook: "waiting", Pane: "permission prompt\n❯ 1. Yes\n  2. No\nEsc to cancel\n"},
			{At: 3 * time.Second, Pane: "Claude is working now\nsome output\n"}, // content changed → running
			{At: 7 * time.Second},  // same content, within 15s cooldown
			{At: 18 * time.Second}, // same content, 15s after the content change
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusWaiting},
			{At: 3 * time.Second, Expected: StatusRunning},
			{At: 7 * time.Second, Expected: StatusRunning},   // cooldown keeps it running
			{At: 18 * time.Second, Expected: StatusWaiting},  // cooldown expired (3s + 15s), falls back
		},
	})
}

func TestScenarioFinishedAutoResumeRunning(t *testing.T) {
	runScenario(t, Scenario{
		Name: "hook=finished but pane shows spinner → override to running",
		Events: []ScenarioEvent{
			{At: 0, Hook: "finished", Pane: "⠋ Thinking...\nctrl+c to interrupt\n"},
		},
		Checks: []ScenarioCheck{
			{At: 0, Expected: StatusRunning},
		},
	})
}
