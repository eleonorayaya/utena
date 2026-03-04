#!/usr/bin/env bash
STATUS_PANES=$(tmux list-panes -F '#{pane_id} #{pane_height} #{@utena-status}' 2>/dev/null | grep ' 1$')

if [ -z "$STATUS_PANES" ]; then
    exit 0
fi

FIRST_PANE=$(echo "$STATUS_PANES" | head -1)
PANE_ID=$(echo "$FIRST_PANE" | awk '{print $1}')
PANE_HEIGHT=$(echo "$FIRST_PANE" | awk '{print $2}')

echo "$STATUS_PANES" | tail -n +2 | while read -r line; do
    extra_id=$(echo "$line" | awk '{print $1}')
    tmux kill-pane -t "$extra_id" 2>/dev/null
done

if [ "$PANE_HEIGHT" -le 1 ]; then
    tmux resize-pane -t "$PANE_ID" -y 8
    tmux select-pane -e -t "$PANE_ID"
    tmux select-pane -t "$PANE_ID"
else
    tmux resize-pane -t "$PANE_ID" -y 1
    tmux select-pane -d -t "$PANE_ID"
    tmux select-pane -t '{up-of}'
fi
