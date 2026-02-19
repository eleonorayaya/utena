package shellinit

func Script() string {
	return `if [ -n "$ZELLIJ_SESSION_NAME" ]; then
  export UTENA_SESSION_ID="$ZELLIJ_SESSION_NAME"
fi
`
}
