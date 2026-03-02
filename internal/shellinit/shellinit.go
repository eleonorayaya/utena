package shellinit

func Script() string {
	return `if [ -n "$TMUX" ]; then
  export UTENA_SESSION_ID="$(tmux display-message -p '#S')"
fi
`
}
