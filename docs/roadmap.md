# brizz-code — Roadmap

*Generated from a comprehensive 5-agent codebase audit, March 2026.*

## Current State

| Dimension | Score | Notes |
|---|---|---|
| Feature depth | 8/10 | Rich, real power-user tool |
| UX & polish | 7/10 | Good key economy, several fixable gaps |
| Code quality | 6/10 | Clean architecture, zero tests, data races |
| Product & distribution | 2/10 | Technically excellent, strategically invisible |
| Reliability | 6/10 | Happy path solid, error paths silently swallow |

## Critical — Fix Now

### Data Races (3 confirmed)
1. `gitInfoCache` — written under `workerMu` in worker, read without lock in `View()` via `selectedRepoInfo()`. Can cause runtime panics.
2. `s.Title` — written without mutex in worker (`app.go:1282`), read by UI in `renderSessionItem()`.
3. `s.tmuxSession` — written in `Restart()` without `s.mu`, read in `UpdateStatus()` from worker goroutine.

### Zero Tests
No `_test.go` files exist anywhere. Priority targets:
- `detectStatus()` — complex multi-pattern matching, documented false-positive history
- `GenerateTitle()` — heuristic with many edge cases
- `stripANSI()` — hand-rolled ANSI parser
- `parseWorktreePorcelain()` — git output parser
- `mergeHookEvent()` — JSON manipulation, idempotency

### Magic Strings in Hook Handler
`hook_handler.go:mapEventToStatus()` returns `"running"`, `"waiting"`, `"finished"` as raw strings instead of `session.Status*` constants. If a constant changes, hooks silently break.

## High Priority — Next 2 Weeks

### UX
- **Restart confirmation** — `r` on a running session immediately kills active work with zero confirmation
- **Rose Pine palette bug** — `Blue` and `Orange` are identical hex `#ebbcba` in `palette.go:72-73`
- **Settings Esc behavior** — Esc currently saves (should be Enter=save, Esc=discard)
- **Workspace name in sidebar** — sessions in different worktrees under the same repo are indistinguishable
- **Finished count in collapsed headers** — collapsed repo headers show running/waiting/error counts but hide finished

### Reliability
- **Hook file permissions** — `0644` (world-readable), contain user prompts. Should be `0600`
- **DB write error logging** — all `_ = h.storage.Update*()` calls silently swallow errors
- **Debug log rotation** — grows unbounded at `~/.config/brizz-code/debug.log`
- **Preview cache cleanup** — entries never removed when sessions are deleted

### Code Quality
- **Duplicate PR badge logic** — identical rendering code in `sidebar.go` and `preview.go`
- **Duplicate HookStatus struct** — `hooks.HookStatus` and `session.HookStatus` exist to avoid import cycle
- **Tmux status bar colors** — hardcoded Tokyo Night. Broken for all other themes

## Medium Priority — Next Month

### Quick Win Features
| Feature | Effort | Impact |
|---|---|---|
| macOS notifications on Waiting/Finished | 2-4h | Eliminates manual checking |
| Scrollable preview pane (Ctrl+U/D) | 2-3h | Review output without attaching |
| Fuzzy/regex filter | 1-2h | Much more useful than substring match |
| Session age in preview ("created 3h ago") | 1h | Temporal context |
| Attention count badge in help bar | 30min | "2 waiting, 1 finished" at a glance |
| g/G to jump top/bottom | 15min | Standard vim-style navigation |

### Visual Polish
| Fix | Effort |
|---|---|
| Distinct symbols: `✓` for Finished, `⟳` for Starting | 30min |
| Help overlay wider (40 → 52 chars) | 5min |
| Filter count indicator ("2/8 matched") | 30min |
| Workspace picker scrolling (currently capped at 10) | 1h |

## Strategic

### Go Public
1. User-facing README with demo GIF
2. GitHub public repo with proper description, topics, license
3. Homebrew tap (`brew install brizz-code`)
4. GoReleaser for automated binary builds
5. Show HN post
6. `brizz-code doctor` command for environment health checks

### Killer Feature Directions
See [vision.md](./vision.md) for detailed descriptions:
1. **AI-powered session digests** — Haiku-generated summaries of what each session accomplished
2. **Event-driven agent spawning** — reactive to CI failures, PR reviews, new tickets
3. **Human context window management** — minimize context-switching cost across features

### Don't Do (Yet)
- Don't rush the desktop app — TUI is the differentiator
- Don't go multi-model — Claude Code hook integration is the moat
- Don't add team collaboration — get individual users first

## Suggested Priority Order

```
NOW (this week):
  1. Fix the 3 data races
  2. Fix Rose Pine palette bug
  3. Add restart confirmation
  4. Fix hook file permissions (0644 → 0600)
  5. Fix Settings Esc behavior

NEXT (next 2 weeks):
  6. Tests for detectStatus, GenerateTitle, stripANSI
  7. Workspace name in sidebar rows
  8. Finished count in collapsed headers
  9. macOS notifications on Waiting/Finished
  10. Log DB write errors instead of swallowing

THEN (next month):
  11. README + GitHub public launch
  12. Homebrew tap
  13. Scrollable preview, fuzzy filter, g/G nav
  14. Distinct status symbols
  15. brizz-code doctor command
```
