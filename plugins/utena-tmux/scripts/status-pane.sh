#!/usr/bin/env bash
SESSION_NAME="${1:-}"
WINDOW_ID="${2:-}"

if [ -z "$SESSION_NAME" ] || [ -z "$WINDOW_ID" ]; then
    exit 0
fi

LOCK_DIR="/tmp/utena-status-pane-${WINDOW_ID}.lock"
if ! mkdir "$LOCK_DIR" 2>/dev/null; then
    exit 0
fi
trap 'rmdir "$LOCK_DIR" 2>/dev/null' EXIT

existing=$(tmux list-panes -t "$WINDOW_ID" -F '#{pane_id} #{@utena-status}' 2>/dev/null | grep ' 1$' | head -1 | awk '{print $1}')
if [ -n "$existing" ]; then
    exit 0
fi

PANE_ID=$(tmux split-window -h -b -f -l 34 -d -t "$WINDOW_ID" -P -F '#{pane_id}' 'utena status')
tmux set-option -p -t "$PANE_ID" @utena-status 1
tmux select-pane -e -t "$PANE_ID"
