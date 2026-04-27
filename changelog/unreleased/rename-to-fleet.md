---
type: changed
---
**Renamed `brizz-code` to `fleet`.** Binary, config dir, tmux prefix, env vars, Chrome native messaging host, and Homebrew formula all renamed:

- Binary: `brizz-code` → `fleet`
- Config dir: `~/.config/brizz-code/` → `~/.config/fleet/`
- Tmux prefix: `brizzcode_` → `fleet_`
- Env vars: `BRIZZCODE_INSTANCE_ID` → `FLEET_INSTANCE_ID`, `BRIZZ_DEBUG` → `FLEET_DEBUG`, `BRIZZ_TELEMETRY_DISABLED` → `FLEET_TELEMETRY_DISABLED`, `BRIZZ_DEMO_PREFIX` → `FLEET_DEMO_PREFIX`
- Per-repo workspace config: `.fleet.json` / `.fleet.local.json` (legacy `.bc.json` / `.bc.local.json` still read for compatibility)
- NMH manifest: `com.brizzai.fleet.tabcontrol.json`
- Homebrew: `brew install brizzai/tap/fleet`

**Auto-migration on first launch:** existing `~/.config/brizz-code/` is moved to `~/.config/fleet/`, live `brizzcode_*` tmux sessions are renamed to `fleet_*`, and stale `brizz-code hook-handler` entries are stripped from `~/.claude/settings.json`. Legacy `BRIZZ*` env vars are accepted as fallback for one release window so in-flight Claude processes survive the upgrade. The Chrome extension keeps the same extension ID (stable via `key` in manifest), so no reinstall is needed.

To upgrade from `brizz-code`:

```bash
brew uninstall brizz-code
brew install brizzai/tap/fleet
```

Or run `fleet` directly — the migration shim handles config moves, tmux session renames, and hook cleanup transparently.
