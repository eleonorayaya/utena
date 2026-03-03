#!/usr/bin/env bash
WINDOW_ID="${1:-}"

if [ -z "$WINDOW_ID" ]; then
    exit 0
fi

PANES=$(tmux list-panes -t "$WINDOW_ID" -F '#{pane_id} #{@utena-status}' 2>/dev/null)
if [ -z "$PANES" ]; then
    exit 0
fi

PANE_COUNT=$(echo "$PANES" | wc -l | tr -d ' ')
if [ "$PANE_COUNT" -eq 1 ]; then
    STATUS_PANE=$(echo "$PANES" | grep ' 1$' | awk '{print $1}')
    if [ -n "$STATUS_PANE" ]; then
        tmux kill-pane -t "$STATUS_PANE"
    fi
fi
