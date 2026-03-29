---
name: debug-status
description: >
  Debug brizz-code session status issues. Traces through the full status pipeline
  (hooks → watcher → UpdateStatus → pane capture) to find where expected and actual
  status diverge. Use when a session shows the wrong status.
allowed-tools: Read, Grep, Glob, Bash, Agent
user-invocable: true
---

# Debug Session Status

Diagnose why a brizz-code session shows the wrong status. Read-only — report findings and recommend fixes, never edit code.

For architecture details, see [reference.md](reference.md).

## Approach

Status flows through a pipeline. Your job is to trace the pipeline and find where it breaks:

```
Claude Code → hook event → hook-handler binary → status file → fsnotify → UpdateStatus() → TUI
                                                                              ↓
                                                                    pane capture (fallback/supplement)
```

At each stage, ask: **what does this stage think the status is, and is it correct?**

## Step 1: Identify the session

Ask the user: which session, what status they see, what they expect.

Find the instance ID and tmux session name:
```
grep "title=<session_title>" ~/.config/brizz-code/debug.log | head -1
```
Extract `session=XXXX` — this is the instance ID and hook filename.

Find the tmux session name (needed for pane capture):
```
tmux list-sessions -F "#{session_name}" | grep brizzcode_ | grep <partial_title>
```
Or query the DB:
```
sqlite3 ~/.config/brizz-code/state.db "SELECT tmux_session_name, title FROM sessions WHERE title LIKE '%<title>%'"
```

## Step 2: What does each layer say?

Check all three layers and compare:

**Hook file** (what hooks last reported):
```
cat ~/.config/brizz-code/hooks/<instance_id>.json
```
Note the `status`, `event`, and `ts` (unix timestamp). How old is it?

**Debug log** (what UpdateStatus decided):
```
grep "<instance_id>" ~/.config/brizz-code/debug.log | grep "status changed" | tail -10
```

**Pane** (what's actually on screen):
```
tmux capture-pane -t <tmux_session_name> -p | tail -20
```
Also check what detectStatus sees:
```
grep "<instance_id>" ~/.config/brizz-code/debug.log | grep "detectStatus" | tail -10
```

## Step 3: Find the divergence

Compare the three layers. The bug is where they disagree:

- **Hook file wrong, pane correct** → Hook event was missed or mapped incorrectly. Check `hook-handler: writing status` entries in the log — is there a gap? Which event should have fired but didn't?

- **Hook file correct, UpdateStatus wrong** → Bug in the `case` logic in `UpdateStatus()`. Read the code path for that hook status in `internal/session/session.go`.

- **Both correct but TUI shows wrong** → Acknowledged/Idle transition issue, or timing (status changed between ticks). Check if `Acknowledged` flag is involved.

- **Pane detection wrong** → Pattern matching issue in `detectStatus()`. Look at what pattern it matched and whether that content is current or stale scrollback.

## Step 4: Check the timeline

```
grep "<instance_id>" ~/.config/brizz-code/debug.log | grep -E "hook-handler|status changed" | tail -30
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
jq -r .session_id ~/.config/brizz-code/hooks/<instance_id>.json
# Then check ~/.claude/projects/*/<session_id>.jsonl
```

**Hook installation** (verify hooks are registered):
```
cat ~/.claude/settings.json | python3 -m json.tool | grep -A5 "brizz-code"
```

**Hook handler execution** (verify binary runs):
```
grep "<instance_id>" ~/.config/brizz-code/debug.log | grep "hook-handler"
```

**Environment variable** (verify hook routing is wired up):
```
tmux show-environment -t <tmux_session_name> BRIZZCODE_INSTANCE_ID
```
If missing or wrong, hooks fire but the handler can't route them to the right session — they silently drop.

## Report

After diagnosis, report:
1. **Expected vs actual**: What status TUI shows vs what's really happening
2. **Divergence point**: Which pipeline stage has the wrong data
3. **Timeline**: Key events with timestamps showing where things went wrong
4. **Root cause**: Why that stage produced wrong output
5. **Recommended fix**: What would prevent this (code change, additional logging, etc.)
