package mux

import "net/http"

type HookEvent struct {
	SessionName string `json:"session_name"`
	Host        string `json:"host,omitempty"`
}

type Activation struct {
	SessionName string
	Host        string
}

func (h *HookEvent) Bind(r *http.Request) error {
	return nil
}

type Window struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}
