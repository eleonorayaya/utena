#!/usr/bin/env bash
EVENT="$1"
SESSION_NAME="${2:-}"
UTENA_PORT="${UTENA_PORT:-3333}"
curl -s -X PUT -H "Content-Type: application/json" \
    -d "{\"session_name\": \"${SESSION_NAME}\"}" \
    "http://localhost:${UTENA_PORT}/tmux/hooks/${EVENT}" \
    >/dev/null 2>&1 || true
