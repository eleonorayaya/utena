package tmux

import "net/http"

type HookEvent struct {
	SessionName string `json:"session_name"`
}

func (h *HookEvent) Bind(r *http.Request) error {
	return nil
}

type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type SyncWindowsRequest struct {
	SessionName string   `json:"session_name"`
	Windows     []Window `json:"windows"`
}

func (s *SyncWindowsRequest) Bind(r *http.Request) error {
	return nil
}
