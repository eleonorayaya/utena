#!/usr/bin/env bash
SESSION_NAME="${1:-}"
UTENA_PORT="${UTENA_PORT:-3333}"

if [ -z "$SESSION_NAME" ]; then
    exit 0
fi

windows_json="["
first=true
while IFS='|' read -r index name active; do
    if [ "$first" = true ]; then
        first=false
    else
        windows_json+=","
    fi
    if [ "$active" = "1" ]; then
        active_val="true"
    else
        active_val="false"
    fi
    windows_json+="{\"index\":${index},\"name\":\"${name}\",\"active\":${active_val}}"
done < <(tmux list-windows -t "$SESSION_NAME" -F '#{window_index}|#{window_name}|#{window_active}' 2>/dev/null)

windows_json+="]"

curl -s -X PUT -H "Content-Type: application/json" \
    -d "{\"session_name\":\"${SESSION_NAME}\",\"windows\":${windows_json}}" \
    "http://localhost:${UTENA_PORT}/tmux/windows" \
    >/dev/null 2>&1 || true
