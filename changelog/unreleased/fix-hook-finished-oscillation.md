---
type: fixed
---
Status detection: sessions with `hook=finished` no longer flap between "running" and "finished" during active sub-agent work. `applyHookFinished` now corroborates pane-detected "finished" with tmux window activity — if the pane was written to in the last 3 seconds, hold the previous state instead of flipping.
