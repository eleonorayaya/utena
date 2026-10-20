#!/bin/bash
set -euo pipefail

DAEMON_URL="http://127.0.0.1:${UTENA_PORT:-3333}"

UTENA_SESSION_ID="${UTENA_SESSION_ID:-}"
if [ -z "$UTENA_SESSION_ID" ] && [ -n "${TUIOS_SESSION:-}" ]; then
    UTENA_SESSION_ID=$(curl -sf --max-time 2 "${DAEMON_URL}/mux/sessions/${TUIOS_SESSION}/env/UTENA_SESSION_ID" 2>/dev/null || true)
fi
if [ -z "$UTENA_SESSION_ID" ]; then
    exit 0
fi

INPUT=$(cat)
HOOK_EVENT=$(echo "$INPUT" | jq -r '.hook_event_name')
CLAUDE_SESSION_ID=$(echo "$INPUT" | jq -r '.session_id')
CWD=$(echo "$INPUT" | jq -r '.cwd')
NOTIFICATION_TYPE=$(echo "$INPUT" | jq -r '.notification_type // empty')

curl -s -X PUT \
    -H "Content-Type: application/json" \
    -d "$(jq -n \
        --arg event "$HOOK_EVENT" \
        --arg claude_session_id "$CLAUDE_SESSION_ID" \
        --arg session_id "$UTENA_SESSION_ID" \
        --arg cwd "$CWD" \
        --arg notification_type "$NOTIFICATION_TYPE" \
        '{event:$event, claude_session_id:$claude_session_id, session_id:$session_id, cwd:$cwd, notification_type:$notification_type}')" \
    "${DAEMON_URL}/claude/hook" > /dev/null 2>&1 || true

exit 0
