---
name: debug-status
description: >
  Debug fleet session status issues. Traces through the full status pipeline
  (hooks → watcher → UpdateStatus → pane capture) to find where expected and actual
  status diverge. Use when a session shows the wrong status.
allowed-tools: Read, Grep, Glob, Bash, Agent
user-invocable: true
---

# Debug Session Status

Diagnose why a fleet session shows the wrong status. Read-only — report findings and recommend fixes, never edit code.

For architecture details, see [reference.md](reference.md).

## Approach

Status flows through a pipeline. Your job is to trace the pipeline and find where it breaks:

```
Claude Code → hook event → hook-handler binary → status file → fsnotify → UpdateStatus() → TUI
                                                                              ↓
                                                                    pane capture (fallback/supplement)
```

At each stage, ask: **what does this stage think the status is, and is it correct?**

## Step 0: Check for snapshots (only if user mentions they took one)

If the user says they captured a snapshot with the `D` hotkey, check it first. Pick the most recent one matching the timeframe they describe:

```bash
ls -lt ~/.config/fleet/snapshots/ | head -10
```

Then read the snapshot — it contains everything you need:

```bash
cat ~/.config/fleet/snapshots/<dir>/snapshot.json   # Session state, hook, detection, mismatch flag
cat ~/.config/fleet/snapshots/<dir>/pane_clean.txt   # Human-readable pane content
cat ~/.config/fleet/snapshots/<dir>/debug_tail.txt   # Last 100 debug log lines for this session
# pane_raw.txt = ANSI-preserved capture, ready to copy to testdata/ for golden tests
```

The `snapshot.json` `detection.mismatch` field tells you immediately if pane detection disagrees with the TUI status. If a snapshot is available, you can often skip directly to Step 3 (Find the divergence).

## Step 1: Identify the session

Ask the user: which session, what status they see, what they expect.

Find the instance ID and tmux session name:
```
grep "title=<session_title>" ~/.config/fleet/debug.log | head -1
```
Extract `session=XXXX` — this is the instance ID and hook filename.

Find the tmux session name (needed for pane capture):
```
tmux list-sessions -F "#{session_name}" | grep fleet_ | grep <partial_title>
```
Or query the DB:
```
sqlite3 ~/.config/fleet/state.db "SELECT tmux_session_name, title FROM sessions WHERE title LIKE '%<title>%'"
```

## Step 2: What does each layer say?

Check all three layers and compare:

**Hook file** (what hooks last reported):
```
cat ~/.config/fleet/hooks/<instance_id>.json
```
Note the `status`, `event`, and `ts` (unix timestamp). How old is it?

**Debug log** (what UpdateStatus decided):
```
grep "<instance_id>" ~/.config/fleet/debug.log | grep "status changed" | tail -10
```

**Pane** (what's actually on screen):
```
tmux capture-pane -t <tmux_session_name> -p | tail -20
```
Also check what detectStatus sees:
```
grep "<instance_id>" ~/.config/fleet/debug.log | grep "detectStatus" | tail -10
```

## Step 3: Find the divergence

Compare the three layers. The bug is where they disagree:

- **Hook file wrong, pane correct** → Hook event was missed or mapped incorrectly. Check `hook-handler: writing status` entries in the log — is there a gap? Which event should have fired but didn't?

- **Hook file correct, UpdateStatus wrong** → Bug in the `case` logic in `UpdateStatus()`. Read the code path for that hook status in `internal/session/session.go`.

- **Both correct but TUI shows wrong** → Acknowledged/Idle transition issue, or timing (status changed between ticks). Check if `Acknowledged` flag is involved.

- **Pane detection wrong** → Pattern matching issue in `detectStatus()`. Look at what pattern it matched and whether that content is current or stale scrollback.

## Step 4: Check the timeline

```
grep "<instance_id>" ~/.config/fleet/debug.log | grep -E "hook-handler|status changed" | tail -30
```

Build a timeline of events. Look for:
- **Gaps**: Long periods with no hook writes while pane shows activity
- **Rapid oscillation**: Status bouncing between two states every few seconds
- **Unexpected sequences**: Events in wrong order, missing expected events

## Step 5: Check for agent team scenarios

If the session uses Claude's agent team feature (sub-agents, `Explore(...)`, `@agent-name`):

**Key symptoms:**
- Hook file shows `Stop/finished` but pane shows a sub-agent permission prompt or "Waiting for team lead approval"
- Status oscillates between running/idle/finished (spinner on "waiting for" line intermittently matches)
- Hook is very stale (hookAge >> minutes) — parent delegated long ago, no new hooks from sub-agent

**What to check:**
- Does the pane show a numbered menu (`❯ 1. Yes`, `2. No`) with `Esc to cancel`? → Should be waiting
- Does the pane show a box (`│ ✻ Waiting for team lead approval │`)? → Should be waiting
- Is the spinner char on a line containing "waiting for"? → Should be skipped by detectRunning
- Is there text containing approval patterns (`(Y/n)`, menu text) in code diffs or conversation output? → False positive source

**Common agent team false-positives:**
- User typed a numbered list as input (`❯ 1. first item`) → looks like permission menu
- Session is editing/discussing status detection code → approval patterns appear in scrollback
- Fix: structural checks require `Esc to cancel` for menu detection, `│` at line start for team box

## Step 6: Go deeper if needed

**Claude conversation log** (verify user actions):
```
# Find the log file
jq -r .session_id ~/.config/fleet/hooks/<instance_id>.json
# Then check ~/.claude/projects/*/<session_id>.jsonl
```

**Hook installation** (verify hooks are registered):
```
cat ~/.claude/settings.json | python3 -m json.tool | grep -A5 "fleet"
```

**Hook handler execution** (verify binary runs):
```
grep "<instance_id>" ~/.config/fleet/debug.log | grep "hook-handler"
```

**Environment variable** (verify hook routing is wired up):
```
tmux show-environment -t <tmux_session_name> FLEET_INSTANCE_ID
```
If missing or wrong, hooks fire but the handler can't route them to the right session — they silently drop.

## Report

After diagnosis, report:
1. **Expected vs actual**: What status TUI shows vs what's really happening
2. **Divergence point**: Which pipeline stage has the wrong data
3. **Timeline**: Key events with timestamps showing where things went wrong
4. **Root cause**: Why that stage produced wrong output
5. **Recommended fix**: What would prevent this (code change, additional logging, etc.)

## Step 7: Add regression test

Every status bug should become a test so it never recurs. The project has two test frameworks for this:

### Golden file test (detection bugs)

For bugs where the pane content was misclassified (wrong `detectStatus` result):

1. **Capture the pane** that triggered the bug:
   ```bash
   tmux capture-pane -t <tmux_session_name> -p -e > internal/session/testdata/<bug-name>.txt
   ```
   The `-e` flag preserves ANSI escape codes — critical for testing the full `stripANSI → detectStatus` pipeline.

2. **Add a test entry** in `internal/session/golden_test.go`:
   ```go
   {"<bug-name>.txt", StatusWaiting, "description of what this pane shows"},
   ```

3. **Verify**: `go test -run TestGoldenDetection -v ./internal/session/` — should FAIL before fix, PASS after.

### Scenario test (state transition bugs)

For bugs where the status machine transitioned incorrectly (e.g., hook said waiting but `applyHookWaiting` overrode to finished):

1. **Add a scenario** in `internal/session/scenario_test.go`:
   ```go
   func TestScenarioBugName(t *testing.T) {
       runScenario(t, Scenario{
           Name: "description of the bug",
           Events: []ScenarioEvent{
               {At: 0, Hook: "waiting", Pane: "content or @fixture:file.txt"},
               // ... sequence of events that trigger the bug
           },
           Checks: []ScenarioCheck{
               {At: 0, Expected: StatusWaiting},
               // ... expected status at each point
           },
       })
   }
   ```

2. **Scenario events** can set: hook status (`Hook`), pane content (`Pane`), pane death (`PaneDead`), user acknowledgement (`Acknowledge`). Pane content can reference a golden fixture with `"@fixture:filename.txt"`.

3. **Scenario checks** assert the session status at a given time offset. The replay engine calls `UpdateStatus()` at each check point using a mock pane capturer (no real tmux needed).

4. **Verify**: `go test -run TestScenarioBugName -v ./internal/session/` — should FAIL before fix, PASS after.

### Which to use?

- **Detection misclassification** (detectStatus returns wrong status for given pane content) → Golden file test
- **State transition error** (correct detection but wrong status due to hook/pane interaction, timing, acknowledged flag) → Scenario test
- **Both** → Add both: golden fixture for the pane + scenario for the transition sequence
