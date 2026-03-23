package claude

import (
	"errors"
	"net/http"
)

type HookEventRequest struct {
	Event            string `json:"event"`
	ClaudeSessionID  string `json:"claude_session_id"`
	SessionID        uint   `json:"session_id,string"`
	CWD              string `json:"cwd,omitempty"`
	NotificationType string `json:"notification_type,omitempty"`
}

func (h *HookEventRequest) Bind(r *http.Request) error {
	if h.Event == "" {
		return errors.New("event is required")
	}
	if h.ClaudeSessionID == "" {
		return errors.New("claude_session_id is required")
	}
	return nil
}

type ClaudeSessionResponse struct {
	*ClaudeSession
}

func NewClaudeSessionResponse(session *ClaudeSession) *ClaudeSessionResponse {
	return &ClaudeSessionResponse{ClaudeSession: session}
}

func (r *ClaudeSessionResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}

type ClaudeSessionListResponse struct {
	ClaudeSessions []ClaudeSession `json:"claude_sessions"`
}

func NewClaudeSessionListResponse(sessions []ClaudeSession) *ClaudeSessionListResponse {
	return &ClaudeSessionListResponse{ClaudeSessions: sessions}
}

func (r *ClaudeSessionListResponse) Render(w http.ResponseWriter, req *http.Request) error {
	return nil
}
