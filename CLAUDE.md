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
internal/session/session.go  # Session model, status detection, claude --resume
internal/session/storage.go  # SQLite persistence (sessions + claude_session_id)
internal/git/git.go          # Git operations (branch, dirty, worktree)
internal/git/repo_info.go    # RepoInfo cache + refresh logic
internal/github/pr.go        # GitHub PR info via gh CLI
internal/hooks/              # Hook-based status detection (claude_hooks, hook_watcher, status_file)
internal/workspace/provider.go     # Provider interface + GitWorktreeProvider + ShellProvider
internal/workspace/repo_config.go  # Per-repo .bc.json loading + ResolveProvider
internal/config/config.go    # JSON config (~/.config/brizz-code/config.json)
internal/debuglog/           # slog-based debug logging to ~/.config/brizz-code/debug.log
internal/ui/                 # Bubble Tea TUI (app, sidebar, preview, dialogs, styles)
internal/ui/palette.go       # Theme palette definitions (5 built-in themes)
internal/ui/settings.go      # Settings dialog (S key)
internal/ui/keybindings.go   # Centralized keybinding definitions
internal/ui/workspace_picker.go  # Workspace picker dialog (provider integration)
internal/ui/workspace_create.go  # Create workspace sub-dialog
```

## Conventions
- Tmux session prefix: `brizzcode_`
- Session ID format: `<8hex>-<unix_timestamp>`
- SQLite DB: `~/.config/brizz-code/state.db`
- Sessions grouped by git repo root in sidebar with tree lines (├─/└─)
- Status: Running, Waiting, Finished, Idle, Error, Starting
- Status icons: ● (running/finished), ◐ (waiting), ○ (idle/starting), ✕ (error)
- Keybindings: j/k nav, Enter attach, Space toggle group, a new (repo-scoped workspace picker), d delete (Y to also destroy workspace), r restart, R rename, e editor, / filter, S settings, ? help, q quit
- Tmux status bar configured per session with detach hint (ctrl+q)
- Attach uses PTY with Ctrl+Q intercept for clean detach (creack/pty + golang.org/x/term)
- Repo headers show branch name (), dirty indicator (*), and PR badge (#N)
- Git info refreshes every 2s (branch/dirty), PR info every 60s via `gh` CLI
- `gh` CLI optional — PR info hidden if not installed
- Status detection: hook-based (primary, no time expiry) via Claude Code hooks + pane capture (fallback, ANSI-stripped)
- All blocking I/O (tmux, git, gh) runs in background worker goroutine, never in Bubble Tea Update()
- Hook status files: `~/.config/brizz-code/hooks/{session_id}.json`
- Hook handler: `brizz-code hook-handler` (invoked by Claude Code hooks, reads BRIZZCODE_INSTANCE_ID env)
- Hooks auto-installed into `~/.claude/settings.json` on TUI launch
- Debug log: `~/.config/brizz-code/debug.log` (slog, init in TUI and hook-handler)
- Config file: `~/.config/brizz-code/config.json` (tick_interval_sec, default_project_path, editor, theme)
- Workspace: built-in git worktree support (zero config), per-repo `.bc.json` overrides with custom shell commands
- `.bc.json` / `.bc.local.json` in repo root: `{"workspace": {"list": "cmd", "create": "cmd {{name}} {{branch}}", "destroy": "cmd {{name}}"}}`
- Claude session resume: captures Claude session_id from hooks, uses `claude --resume <id>` on restart
- Editor: config.editor > $EDITOR > "code" (VS Code)
- Themes: tokyo-night (default), catppuccin-mocha, rose-pine, nord, gruvbox — configurable via settings (S key)
- Settings dialog: S key opens settings overlay, live theme preview, auto-saves on close
- Claude Code only, Mac only
