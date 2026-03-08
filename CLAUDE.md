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
internal/tmux/tmux.go        # Tmux abstraction (create, attach, kill, capture)
internal/session/session.go  # Session model, status detection
internal/session/storage.go  # SQLite persistence
internal/git/git.go          # Git operations (branch, dirty, worktree)
internal/git/repo_info.go    # RepoInfo cache + refresh logic
internal/github/pr.go        # GitHub PR info via gh CLI
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
- Claude Code only, Mac only
