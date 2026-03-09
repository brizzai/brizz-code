# Status Detection — Architecture Reference

## Pipeline

```
Claude Code hooks → hook-handler CLI → status file → fsnotify → HookWatcher → Session.UpdateStatus()
                                                                                    ↓
                                                                   pane capture + content hashing
```

## Status Model

| Status   | Icon | Meaning                                |
|----------|------|----------------------------------------|
| Starting | ○    | Session just created, tmux initializing |
| Running  | ●    | Claude actively processing              |
| Waiting  | ◐    | Permission/approval prompt showing      |
| Finished | ●    | Claude done, not yet acknowledged       |
| Idle     | ○    | Claude done, user acknowledged          |
| Error    | ✕    | Session crashed or tmux died            |

## Hook Event → Status Mapping

Defined in `cmd/brizz-code/hook_handler.go` `mapEventToStatus()`.

| Event             | Matcher                                | → Status  |
|-------------------|----------------------------------------|-----------|
| UserPromptSubmit  |                                        | running   |
| Stop              |                                        | finished  |
| PermissionRequest |                                        | waiting   |
| Notification      | `permission_prompt\|elicitation_dialog` | waiting   |
| Notification      | `idle_prompt`                          | finished  |
| SessionStart      |                                        | finished  |
| SessionEnd        |                                        | dead      |

## UpdateStatus() — Key Code Paths

Located in `internal/session/session.go`. Runs every tick (~2s).

Each `case` in the hook switch has different override/fallback behavior:
- Some cases allow pane overrides, some don't
- Some track content hash changes, some reset the hash
- The `Acknowledged` flag affects finished→idle transition

Read the actual code for current behavior — it changes as bugs are fixed.

## Pane Detection (detectStatus)

Scans last 50 non-empty lines of `tmux capture-pane` output after ANSI stripping.

**Priority order** (first match wins):
1. Busy patterns → Running
2. Spinner characters → Running
3. Whimsical activity (… + tokens) → Running
4. Approval patterns → Waiting
5. Prompt indicators (❯ or >) → Finished
6. Idle patterns → Finished
7. No match → unknown (keeps previous)

**Fundamental limitation**: Pane is a flat text buffer. It contains stale content from previous turns mixed with current state. Pattern matching cannot reliably distinguish current from stale.

## Key Files

| File | What to look for |
|------|------------------|
| `internal/session/session.go` | UpdateStatus(), detectStatus(), Status type |
| `cmd/brizz-code/hook_handler.go` | mapEventToStatus(), hook payload parsing |
| `internal/hooks/claude_hooks.go` | Hook installation, event configs |
| `internal/hooks/hook_watcher.go` | fsnotify watcher |
| `internal/hooks/status_file.go` | Status file format |

## Key Logs

All in `~/.config/brizz-code/debug.log`:

| Log message | Meaning |
|-------------|---------|
| `hook-handler: writing status` | Hook handler received event, writing file |
| `hook-handler: no BRIZZCODE_INSTANCE_ID` | Env var missing, can't route hook |
| `hook-handler: unmapped event` | Event type not in mapEventToStatus |
| `status changed (hook)` | UpdateStatus changed status based on hook data |
| `status changed (pane)` | UpdateStatus changed status based on pane capture |
| `detectStatus: matched ...` | What pane pattern was detected |
| `content changed while waiting` | Content hash changed during waiting state |
| `content stable >10s` | Content hasn't changed for 10s during running state |

## Hook Status Files

Location: `~/.config/brizz-code/hooks/<instance_id>.json`

```json
{"status":"running","session_id":"<claude-uuid>","event":"UserPromptSubmit","ts":1773090529}
```

One file per brizz-code session. Last-write-wins. Cleaned up after 24h.
