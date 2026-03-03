#!/usr/bin/env bash
EVENT="$1"
SESSION_NAME="${2:-}"
UTENA_PORT="${UTENA_PORT:-3333}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

curl -s -X PUT -H "Content-Type: application/json" \
    -d "{\"session_name\": \"${SESSION_NAME}\"}" \
    "http://localhost:${UTENA_PORT}/tmux/hooks/${EVENT}" \
    >/dev/null 2>&1 || true

if [ "$EVENT" = "session-created" ] || [ "$EVENT" = "client-session-changed" ]; then
    "$SCRIPT_DIR/sync-windows.sh" "$SESSION_NAME"
fi

if [ "$EVENT" = "session-created" ]; then
    tmux list-windows -t "$SESSION_NAME" -F '#{window_id}' 2>/dev/null | while read -r window_id; do
        "$SCRIPT_DIR/status-pane.sh" "$SESSION_NAME" "$window_id"
    done
fi
