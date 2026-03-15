<p align="center">
  <h1 align="center">brizz-code</h1>
  <p align="center">
    <strong>Run 10 Claude Code agents. Stay sane.</strong>
  </p>
  <p align="center">
    A terminal UI for orchestrating multiple Claude Code sessions in parallel.
    <br />
    See which agents need you. Jump in, direct, jump out.
  </p>
</p>

<br />

## The Problem

You're running 5 Claude Code sessions across different features. One is waiting for approval, two finished while you weren't looking, and you can't remember what the fourth one was doing.

**brizz-code** gives you a single screen to see everything, jump to what matters, and get back to orchestrating.

## How It Works

```
┌─ sidebar ──────────────┬─ preview ────────────────────────┐
│                        │                                  │
│ myapp/                 │  Session: Add auth middleware     │
│ ├─ ● Add auth midlwr  │  Status:  Running                │
│ ├─ ◐ Fix login bug     │  Branch:  feat/auth              │
│ └─ ● Setup CI          │  PR:      #42 ✓                  │
│                        │                                  │
│ api-service/           │  > Implementing JWT refresh      │
│ └─ ○ Refactor handlers │    token rotation...             │
│                        │                                  │
└────────────────────────┴──────────────────────────────────┘
 ● running  ◐ waiting  ○ idle  ✕ error     Space: next attention
```

- **Sessions grouped by repo** with branch, dirty state, and PR badges
- **Status detection** via Claude Code hooks — no polling, no guessing
- **`Space`** jumps to the next session that needs your attention
- **`Enter`** attaches to a session, **`Ctrl+Q`** detaches cleanly
- **`Y`** quick-approves a waiting prompt without attaching
- **Auto-naming** — sessions title themselves from your first prompt

## Install

```bash
# Clone and run the installer
git clone git@github.com:brizzai/brizz-code.git /tmp/brizz-code
bash /tmp/brizz-code/install.sh
```

### Requirements

- macOS
- [`gh`](https://cli.github.com/) authenticated with repo access (`gh auth login`)
- [tmux](https://github.com/tmux/tmux) (`brew install tmux`)
- [Claude Code](https://docs.anthropic.com/en/docs/claude-code)
- Git

## Quick Start

```bash
# Launch the TUI
brizz-code

# Press 'n' to create a new session in a workspace
# Press 'a' to quick-add a session in the current repo
# Press '?' for all keybindings
```

## Keybindings

| Key | Action |
|-----|--------|
| `j` / `k` | Navigate up/down |
| `Enter` | Attach to session |
| `Ctrl+Q` | Detach from session |
| `Space` | Jump to next waiting/finished session |
| `a` | New session (current repo) |
| `n` | New session (workspace picker) |
| `Y` | Quick approve waiting prompt |
| `d` | Delete session |
| `r` | Restart session |
| `R` | Rename session |
| `f` | Fork session |
| `b` | Switch git branch |
| `e` | Open in editor |
| `p` | Open PR in browser |
| `/` | Filter sessions |
| `S` | Settings |
| `?` | Help |
| `q` | Quit |

## Themes

5 built-in themes, switchable from settings (`S`):

**tokyo-night** (default) · **catppuccin-mocha** · **rose-pine** · **nord** · **gruvbox**

## How It Fits

```
You (orchestrator)
 └─ brizz-code (awareness + control)
      ├─ Claude Code session → feat/auth branch
      ├─ Claude Code session → fix/login-bug branch
      ├─ Claude Code session → feat/ci-setup branch
      └─ ...
```

Each session is one agent, one branch, one task. Claude handles the coding — commits, PRs, tests. You handle the directing. brizz-code is your cockpit.

## License

MIT
