#!/usr/bin/env bash
SESSION_NAME="${1:-}"
WINDOW_ID="${2:-}"

if [ -z "$SESSION_NAME" ] || [ -z "$WINDOW_ID" ]; then
    exit 0
fi

existing=$(tmux list-panes -t "$WINDOW_ID" -F '#{pane_id} #{@utena-status}' 2>/dev/null | grep ' 1$' | head -1 | awk '{print $1}')
if [ -n "$existing" ]; then
    exit 0
fi

PANE_ID=$(tmux split-window -v -f -l 1 -d -t "$WINDOW_ID" -P -F '#{pane_id}' 'utena status')
tmux set-option -p -t "$PANE_ID" @utena-status 1
tmux select-pane -d -t "$PANE_ID"
