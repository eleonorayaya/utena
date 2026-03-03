#!/usr/bin/env bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

tmux list-windows -a -F '#{session_name} #{window_id}' 2>/dev/null | while read -r session_name window_id; do
    "$SCRIPT_DIR/status-pane.sh" "$session_name" "$window_id"
done
