#!/usr/bin/env bash
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

STATUS_PANES=$(tmux list-panes -F '#{pane_id} #{pane_width} #{@utena-status}' 2>/dev/null | grep ' 1$')

if [ -z "$STATUS_PANES" ]; then
    SESSION_NAME=$(tmux display-message -p '#{session_name}')
    WINDOW_ID=$(tmux display-message -p '#{window_id}')
    "$SCRIPT_DIR/status-pane.sh" "$SESSION_NAME" "$WINDOW_ID"
    exit 0
fi

FIRST_PANE=$(echo "$STATUS_PANES" | head -1)
PANE_ID=$(echo "$FIRST_PANE" | awk '{print $1}')
PANE_WIDTH=$(echo "$FIRST_PANE" | awk '{print $2}')

echo "$STATUS_PANES" | tail -n +2 | while read -r line; do
    extra_id=$(echo "$line" | awk '{print $1}')
    tmux kill-pane -t "$extra_id" 2>/dev/null
done

if [ "$PANE_WIDTH" -le 14 ]; then
    tmux resize-pane -t "$PANE_ID" -x 34
    tmux select-pane -t "$PANE_ID"
else
    tmux resize-pane -t "$PANE_ID" -x 14
    tmux select-pane -t '{right-of}'
fi
