# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.3.0] - 2026-04-15

### Added

- RTS-style session hotkeys: bind the selected session to a numbered slot with `Alt+0-9` (or `=` then a digit), jump with plain `0-9`, double-tap within 400ms to also attach. Unbind by re-pressing `Alt+<N>` on the already-bound session, or `==` then the digit to clear any slot. Bound sessions show a `[N]` badge in the sidebar and persist across restarts.
- Undo delete (z key): restore deleted sessions within 5 seconds. Sticky repos: empty repo groups persist in sidebar until dismissed.

### Fixed

- Fatal "concurrent map read and map write" crash caused by unlocked reads of the git info cache during render
- Status detection: sessions with `hook=finished` no longer flap between "running" and "finished" during active sub-agent work. `applyHookFinished` now corroborates pane-detected "finished" with tmux window activity — if the pane was written to in the last 3 seconds, hold the previous state instead of flipping.
- Status detection: permission menus where the cursor is on option 2 or 3 (not just option 1) are now correctly detected as "waiting" instead of flipping the session to idle.
- Status detection: sessions running Explore sub-agents (with the `· ↑ tokens` output counter) stay marked as "running" instead of collapsing to "idle" when a stale waiting hook is in play.
- Status detection: idle sessions no longer get stuck at "running" when their scrollback contains text that mentions the whimsical token counter (e.g. commit messages or docs referencing `· ↓`/`· ↑` + `tokens`).

## [1.2.0] - 2026-04-09

## [1.2.0] - 2026-04-09

### Added

- Agent team status detection: sub-agent permission prompts and "Waiting for team lead approval" now correctly show as waiting
- Command palette (`:` or `Ctrl+P`) — fuzzy-searchable list of all actions with shortcut hints, plus "Reload All Sessions" for bulk restart of dead/error sessions
- Terminal environment and rendering stats in bug reports to help diagnose scroll/rendering issues

### Improved

- Status updates now respond in ~150ms instead of up to 2s via event-driven hook notifications

### Fixed

- Agent team sessions showing idle/running instead of waiting when sub-agent needs approval
- Bug report dialog freezing permanently when `gh` CLI is not installed
- "Last used" time now updates on all interactions (approve, restart, new prompt), not just attach
- Status showing stale data immediately after detaching from a session
- Status oscillating between idle and finished when stale waiting hook is present
- Session stuck at "waiting" status after user interrupts/escapes a permission prompt


## [1.1.0] - 2026-03-21

### Added

- Anonymous usage analytics to help improve fleet (opt out via Settings, config, or `DO_NOT_TRACK=1`)

## [1.0.0] - 2026-03-21

Initial open-source release.

### Added

- TUI for managing multiple Claude Code sessions in parallel using tmux
- Real-time status detection via Claude Code hooks (no polling)
- Sessions grouped by git repo with branch name, dirty indicator, and PR badges
- Jump to next waiting session (`Space`) and quick approve (`Y`)
- Git worktree integration with branch picker (`w`)
- Session fork to branch off Claude conversations (`f`)
- Session resume with `claude --resume` on restart
- Auto-naming sessions from first user prompt
- 5 built-in themes: tokyo-night, catppuccin-mocha, rose-pine, nord, gruvbox
- Settings dialog with live theme preview (`S`)
- Full PTY attach with Ctrl+Q detach and split/focus mode
- Chrome extension for tab control (reuse PR tabs with `p`)
- Bug report dialog with diagnostics, error history, and action log (`!`)
- Auto-update mechanism with `fleet update`
- Install via Homebrew, shell script, or `go install`
- Per-repo workspace config via `.bc.json` / `.bc.local.json`
- `/ship` release workflow — comment `/ship` on any issue or PR to release
- Changelog check on PRs with `/no-changelog` escape hatch

[Unreleased]: https://github.com/brizzai/fleet/compare/v1.3.0...HEAD
[1.3.0]: https://github.com/brizzai/fleet/releases/tag/v1.3.0
[1.2.0]: https://github.com/brizzai/fleet/releases/tag/v1.2.0
[1.1.0]: https://github.com/brizzai/fleet/releases/tag/v1.1.0
[1.0.0]: https://github.com/brizzai/fleet/releases/tag/v1.0.0
