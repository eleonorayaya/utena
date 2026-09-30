package shellinit

func Script() string {
	return `if [ -n "$TUIOS_ENV" ]; then
  unset UTENA_SESSION_ID
  _utena_val=$(curl -sf --max-time 1 "http://localhost:${UTENA_PORT:-3333}/tmux/sessions/${TUIOS_SESSION}/env/UTENA_SESSION_ID" 2>/dev/null)
  if [ -n "$_utena_val" ]; then
    export UTENA_SESSION_ID="$_utena_val"
  fi
  unset _utena_val
fi
`
}
