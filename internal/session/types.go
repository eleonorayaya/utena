package session

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
)

type SessionResponse struct {
	*Session
}

func NewSessionResponse(session *Session) *SessionResponse {
	return &SessionResponse{Session: session}
}

func (sr *SessionResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

type SessionListResponse struct {
	Sessions []*SessionResponse `json:"sessions"`
}

func NewSessionListResponse(sessions []Session) *SessionListResponse {
	resp := make([]*SessionResponse, len(sessions))
	for i := range sessions {
		resp[i] = NewSessionResponse(&sessions[i])
	}
	return &SessionListResponse{Sessions: resp}
}

func (slr *SessionListResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func RenderSessionList(sessions []Session) []render.Renderer {
	list := make([]render.Renderer, len(sessions))
	for i, session := range sessions {
		s := session
		list[i] = NewSessionResponse(&s)
	}
	return list
}

type CreateSessionRequest struct {
	Name           string `json:"name,omitempty"`
	WorkspaceID    uint   `json:"workspace_id"`
	Branch         string `json:"branch,omitempty"`
	BaseBranch     string `json:"base_branch,omitempty"`
	CreateWorktree bool   `json:"create_worktree"`
}

func (c *CreateSessionRequest) Bind(r *http.Request) error {
	if c.WorkspaceID == 0 {
		return errors.New("workspace_id is required")
	}
	if c.Name != "" {
		return ValidateSessionName(c.Name)
	}
	return nil
}

type UpdateSessionRequest struct {
	*Session
}

func (u *UpdateSessionRequest) Bind(r *http.Request) error {
	if u.Session == nil {
		return errors.New("session cannot be nil")
	}
	return ValidateSession(u.Session)
}
