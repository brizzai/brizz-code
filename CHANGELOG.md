# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `/ship` release workflow — comment `/ship` on any issue or PR to release
- Changelog check on PRs with `/no-changelog` escape hatch

## [1.0.0] - YYYY-MM-DD

### Added

- TUI for managing multiple Claude Code sessions in parallel
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

[Unreleased]: https://github.com/brizzai/brizz-code/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/brizzai/brizz-code/releases/tag/v1.0.0
