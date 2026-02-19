#!/bin/bash
set -euo pipefail

UTENA_SESSION_ID="${UTENA_SESSION_ID:-}"
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
    "http://localhost:3333/claude/hook" > /dev/null 2>&1 || true

exit 0
