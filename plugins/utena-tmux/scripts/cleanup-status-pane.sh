#!/usr/bin/env bash
WINDOW_ID="${1:-}"

if [ -z "$WINDOW_ID" ]; then
    exit 0
fi

PANES=$(tmux list-panes -t "$WINDOW_ID" -F '#{pane_id} #{@utena-status}' 2>/dev/null)
if [ -z "$PANES" ]; then
    exit 0
fi

NON_STATUS_COUNT=$(echo "$PANES" | grep -v ' 1$' | grep -c . || true)
if [ "$NON_STATUS_COUNT" -eq 0 ]; then
    echo "$PANES" | grep ' 1$' | awk '{print $1}' | while read -r status_pane; do
        tmux kill-pane -t "$status_pane" 2>/dev/null
    done
fi
