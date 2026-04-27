---
type: fixed
---
Status detection: hook events now reflect in the TUI within ~100ms (was 4–6s, waiting on the worker's round-robin). Stale "running" hooks no longer oscillate between idle/running/finished on pane-content changes (survey popups, cursor blinks, scrollback redraws).
