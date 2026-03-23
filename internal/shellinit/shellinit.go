package shellinit

func Script() string {
	return `if [ -n "$TMUX" ]; then
  _utena_val=$(tmux show-environment UTENA_SESSION_ID 2>/dev/null)
  if [ -n "$_utena_val" ] && [ "${_utena_val#-}" = "$_utena_val" ]; then
    export "$_utena_val"
  fi
  unset _utena_val
fi
`
}
