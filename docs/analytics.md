# Analytics

brizz-code collects anonymous usage analytics via [Amplitude](https://amplitude.com) to understand how the tool is used and prioritize development.

## What We Collect

### Events

| Event | Properties | When |
|---|---|---|
| `app_started` | `version`, `session_count`, `repo_count` | TUI launches |
| `app_quit` | `uptime_seconds` | TUI exits |
| `session_created` | `method` (fork) | New session started |
| `session_attached` | — | User enters a session |
| `session_restarted` | — | Session restarted |
| `session_deleted` | — | Session deleted |
| `session_renamed` | — | Session renamed |
| `quick_approve` | — | Y key pressed |
| `editor_opened` | `editor` (e.g. "code", "nvim") | e key pressed |
| `pr_opened` | — | p key pressed |
| `workspace_created` | `provider` ("git" or "shell") | Worktree/workspace created |
| `theme_changed` | `theme` (e.g. "tokyo-night") | Theme cycled in settings |
| `filter_used` | — | / key pressed |
| `space_jump` | — | Space key pressed |
| `error_occurred` | `category` (error prefix) | Any error shown |
| `settings_opened` | — | S key pressed |
| `bug_report_opened` | — | ! key pressed |

### User Properties (set once per launch)

| Property | Example |
|---|---|
| `app_version` | `v1.0.0` |
| `os_version` | `15.3` |
| `arch` | `arm64` |
| `theme` | `tokyo-night` |
| `enter_mode` | `attach` |
| `auto_name_sessions` | `true` |
| `copy_claude_settings` | `true` |

### Derived Metrics

From the above events, Amplitude automatically provides:
- **DAU / MAU** — unique devices per day/month (from `app_started`)
- **Feature adoption** — % of users using each feature
- **Session patterns** — sessions per user, uptime distribution
- **Error rates** — error frequency by category
- **Retention** — returning users over time

## What We Do NOT Collect

- File paths or project names
- Code content or prompts
- Usernames, emails, or any PII
- Git branch names or commit hashes
- Session titles or content
- IP addresses (Amplitude anonymizes by default)

## Privacy

### Anonymous Device ID

Each installation generates a **one-way SHA256 hash** of the macOS hardware UUID. This hash:
- Cannot be reversed to identify you or your machine
- Is stable across app updates (cached at `~/.config/brizz-code/device_id`)
- Is the only identifier sent to Amplitude

### No Network Impact

Analytics events are batched (queue size: 20) and flushed asynchronously. The SDK logger is silenced — no output to stdout or stderr.

## How to Opt Out

Any of these methods will completely disable analytics:

### 1. Environment Variable

```bash
export BRIZZ_TELEMETRY_DISABLED=1
```

Or use the standard [Do Not Track](https://consoledonottrack.com/) convention:

```bash
export DO_NOT_TRACK=1
```

### 2. Config File

Edit `~/.config/brizz-code/config.json`:

```json
{
  "telemetry": false
}
```

### 3. Settings Dialog

Press `S` in the TUI and toggle **Telemetry** to **off**.

## Architecture

```
internal/analytics/
├── analytics.go   # Client, init, track, shutdown, device ID, opt-out
└── events.go      # Event name constants
```

- **Global singleton** — `Init()` creates once, `Track()` / `Shutdown()` are safe to call from anywhere
- **No-op when disabled** — all `Track` calls return immediately if opted out
- **Thread-safe** — protected by mutex, safe for concurrent use from TUI goroutines
- **Silent** — custom `silentLogger` suppresses all Amplitude SDK output
