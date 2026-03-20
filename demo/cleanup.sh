#!/bin/bash
set -euo pipefail

DEMO_DIR="/tmp/brizz-demo"
BRIZZ="./build/brizz-code"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$ROOT_DIR"

echo "=== brizz-code demo cleanup ==="

# Kill tmux sessions whose working directory is under the demo dir.
for sess in $(tmux list-sessions -F '#{session_name}' 2>/dev/null | grep '^brizzcode_' || true); do
    pane_path=$(tmux display-message -t "$sess" -p '#{pane_current_path}' 2>/dev/null || echo "")
    if [[ "$pane_path" == "$DEMO_DIR"* ]]; then
        echo "Killing tmux session: $sess"
        tmux kill-session -t "$sess" 2>/dev/null || true
    fi
done

# Remove demo sessions from the database.
if [ -x "$BRIZZ" ]; then
    for id in $($BRIZZ list 2>/dev/null | grep "$DEMO_DIR" | awk '{print $1}' || true); do
        if [ -n "$id" ] && [ "$id" != "ID" ]; then
            echo "Removing session: $id"
            $BRIZZ remove "$id" 2>/dev/null || true
        fi
    done
fi

# Remove demo repos.
if [ -d "$DEMO_DIR" ]; then
    echo "Removing $DEMO_DIR"
    rm -rf "$DEMO_DIR"
fi

echo "Done."
