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
internal/naming/naming.go    # Auto-name sessions via smart heuristic (filler stripping, title-case)
internal/debuglog/           # slog-based debug logging to ~/.config/brizz-code/debug.log
internal/ui/                 # Bubble Tea TUI (app, sidebar, preview, dialogs, styles)
internal/ui/palette.go       # Theme palette definitions (5 built-in themes)
internal/ui/settings.go      # Settings dialog (S key)
internal/ui/keybindings.go   # Centralized keybinding definitions
internal/ui/workspace_picker.go  # Workspace picker dialog (provider integration)
internal/ui/workspace_create.go  # Create workspace sub-dialog + PendingWorkspace phantom entries
internal/chrome/protocol.go      # Command/Response types, action constants, socket path
internal/chrome/native_host.go   # Native messaging host with Unix socket bridge
internal/chrome/client.go        # TUI-side client (connects to socket, sends commands)
internal/chrome/install.go       # NMH manifest auto-install to Chrome's NativeMessagingHosts dir
chrome-extension/                # Chrome MV3 extension (service worker, manifest, icons)
```

## Conventions
- Tmux session prefix: `brizzcode_`
- Session ID format: `<8hex>-<unix_timestamp>`
- SQLite DB: `~/.config/brizz-code/state.db`
- Sessions grouped by git repo root in sidebar with tree lines (├─/└─)
- Status: Running, Waiting, Finished, Idle, Error, Starting
- Status icons: ● (running/finished), ◐ (waiting), ○ (idle/starting), ✕ (error)
- Keybindings: j/k nav, Enter attach, Space jump to next waiting/finished, a new session (instant, repo-scoped), n new session in workspace (picker), d delete (Y to also destroy workspace), r restart, R rename, e editor, p open PR in browser, Y quick approve (waiting sessions), / filter, S settings, ? help, q quit
- Tmux status bar configured per session with detach hint (ctrl+q)
- Attach uses PTY with Ctrl+Q intercept for clean detach (creack/pty + golang.org/x/term)
- Repo headers show branch name (), dirty indicator (*), and PR badge (#N)
- Git info refreshes every 2s (branch/dirty), PR info every 60s via `gh` CLI
- PR badge: green ✓ (approved+CI passed), yellow (pending), red ✕ (CI fail) / ↩ (changes requested or unresolved threads), purple ⇡ (merged), hidden (closed)
- PR info includes unresolved review thread count via GitHub GraphQL API
- `gh` CLI optional — PR info hidden if not installed
- Preview strips OSC-8 hyperlink sequences to prevent dotted underline artifacts
- Status detection: hook-based (primary, no time expiry) via Claude Code hooks + pane capture (fallback, ANSI-stripped)
- All blocking I/O (tmux, git, gh) runs in background worker goroutine, never in Bubble Tea Update()
- Hook status files: `~/.config/brizz-code/hooks/{session_id}.json`
- Hook handler: `brizz-code hook-handler` (invoked by Claude Code hooks, reads BRIZZCODE_INSTANCE_ID env)
- Hooks auto-installed into `~/.claude/settings.json` on TUI launch
- Debug log: `~/.config/brizz-code/debug.log` (slog, init in TUI and hook-handler)
- Config file: `~/.config/brizz-code/config.json` (tick_interval_sec, default_project_path, editor, theme, auto_name_sessions)
- Workspace: built-in git worktree support (zero config), per-repo `.bc.json` overrides with custom shell commands
- Workspace creation is non-blocking: dialog closes immediately, phantom "Creating..." entry with spinner appears in sidebar, user can keep navigating
- `.bc.json` / `.bc.local.json` in repo root: `{"workspace": {"list": "cmd", "create": "cmd {{name}} {{branch}}", "destroy": "cmd {{name}}"}}`
- Claude session resume: captures Claude session_id from hooks, uses `claude --resume <id>` on restart
- Editor: config.editor > $EDITOR > "code" (VS Code)
- Themes: tokyo-night (default), catppuccin-mocha, rose-pine, nord, gruvbox — configurable via settings (S key)
- Settings dialog: S key opens settings overlay, live theme preview, auto-name toggle, auto-saves on close
- Auto-naming: sessions auto-titled from user prompt via smart heuristic (filler stripping, word-boundary truncation)
- Auto-naming pipeline: UserPromptSubmit hook → status file → HookWatcher → Session.FirstPrompt → worker cycle → naming.GenerateTitle
- Retitle: after 3 prompts, title regenerated from latest prompt (better reflects session scope)
- Manual rename (R key) sets ManuallyRenamed flag, prevents auto-rename
- Chrome tab control: `p` opens PR in Chrome via extension (reuses existing tab), falls back to `open <url>` if unavailable
- Chrome extension architecture: TUI →[unix socket]→ native host (`brizz-code chrome-host`) →[stdio]→ Chrome service worker
- Native messaging host: `brizz-code chrome-host` subcommand (also auto-detected when Chrome passes `chrome-extension://...` arg)
- Unix socket: `~/.config/brizz-code/chrome.sock` (created by native host, mode 0600)
- NMH manifest: auto-installed to `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/com.brizzcode.tabcontrol.json` on TUI startup
- Chrome extension ID: `haphpcoecelhofejcklinnlbfijgdnih` (stable via `key` in manifest.json)
- Extension commands: `open_or_focus`, `close_tab`, `create_tab_group`, `ping`
- Service worker reconnects to native host on disconnect (2s delay)
- Claude Code only, Mac only
