# brizz-code

TUI tool for managing multiple Claude Code sessions in parallel using tmux.

## Tech Stack
- Go 1.24+, Bubble Tea + Lipgloss, tmux, SQLite (WAL mode)

## Build
```
make build    # build to build/brizz-code
make run      # go run
make test     # go test -race
make fmt      # go fmt
```

## Package Structure
```
cmd/brizz-code/main.go      # CLI entry point
internal/tmux/tmux.go        # Tmux abstraction (create, kill, capture)
internal/tmux/pty.go         # PTY-based attach with Ctrl+Q detach
internal/session/session.go  # Session model, status detection
internal/session/storage.go  # SQLite persistence
internal/git/git.go          # Git operations (branch, dirty, worktree)
internal/git/repo_info.go    # RepoInfo cache + refresh logic
internal/github/pr.go        # GitHub PR info via gh CLI
internal/hooks/              # Hook-based status detection (claude_hooks, hook_watcher, status_file)
internal/ui/                 # Bubble Tea TUI (app, sidebar, preview, dialogs, styles)
```

## Conventions
- Tmux session prefix: `brizzcode_`
- Session ID format: `<8hex>-<unix_timestamp>`
- SQLite DB: `~/.config/brizz-code/state.db`
- Sessions grouped by git repo root in sidebar with tree lines (├─/└─)
- Status: Running, Waiting, Finished, Idle, Error, Starting
- Status icons: ● (running/finished), ◐ (waiting), ○ (idle/starting), ✕ (error)
- Keybindings: j/k nav, Enter attach, a new, d delete, r restart, ? help, q quit
- Tmux status bar configured per session with detach hint (ctrl+q)
- Attach uses PTY with Ctrl+Q intercept for clean detach (creack/pty + golang.org/x/term)
- Repo headers show branch name (), dirty indicator (*), and PR badge (#N)
- Git info refreshes every 2s (branch/dirty), PR info every 60s via `gh` CLI
- `gh` CLI optional — PR info hidden if not installed
- Status detection: hook-based (primary) via Claude Code hooks + pane capture (fallback)
- Hook status files: `~/.config/brizz-code/hooks/{session_id}.json`
- Hook handler: `brizz-code hook-handler` (invoked by Claude Code hooks, reads BRIZZCODE_INSTANCE_ID env)
- Hooks auto-installed into `~/.claude/settings.json` on TUI launch
- Claude Code only, Mac only
