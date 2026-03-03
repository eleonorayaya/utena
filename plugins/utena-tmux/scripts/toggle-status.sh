#!/usr/bin/env bash
STATUS_PANE=$(tmux list-panes -F '#{pane_id} #{pane_height} #{@utena-status}' 2>/dev/null | grep ' 1$' | head -1)

if [ -z "$STATUS_PANE" ]; then
    exit 0
fi

PANE_ID=$(echo "$STATUS_PANE" | awk '{print $1}')
PANE_HEIGHT=$(echo "$STATUS_PANE" | awk '{print $2}')

if [ "$PANE_HEIGHT" -le 1 ]; then
    tmux resize-pane -t "$PANE_ID" -y 8
    tmux select-pane -e -t "$PANE_ID"
    tmux select-pane -t "$PANE_ID"
else
    tmux resize-pane -t "$PANE_ID" -y 1
    tmux select-pane -d -t "$PANE_ID"
    tmux select-pane -t '{up-of}'
fi
