package tmux

import "net/http"

type HookEvent struct {
	SessionName string `json:"session_name"`
}

func (h *HookEvent) Bind(r *http.Request) error {
	return nil
}
