# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Terminal environment and rendering stats in bug reports to help diagnose scroll/rendering issues
- Agent team status detection: sub-agent permission prompts and "Waiting for team lead approval" now correctly show as waiting

### Fixed

- Bug report dialog freezing permanently when `gh` CLI is not installed
- Session stuck at "waiting" status after user interrupts/escapes a permission prompt
- Agent team sessions showing idle/running instead of waiting when sub-agent needs approval
- Status oscillating between idle and finished when stale waiting hook is present
- Temp file not cleaned up when bug report body write fails

## [1.1.0] - 2026-03-21

### Added

- Anonymous usage analytics to help improve brizz-code (opt out via Settings, config, or `DO_NOT_TRACK=1`)

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
- Auto-update mechanism with `brizz-code update`
- Install via Homebrew, shell script, or `go install`
- Per-repo workspace config via `.bc.json` / `.bc.local.json`
- `/ship` release workflow — comment `/ship` on any issue or PR to release
- Changelog check on PRs with `/no-changelog` escape hatch

[Unreleased]: https://github.com/brizzai/brizz-code/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/brizzai/brizz-code/releases/tag/v1.1.0
[1.0.0]: https://github.com/brizzai/brizz-code/releases/tag/v1.0.0
