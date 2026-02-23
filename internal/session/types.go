package session

import (
	"errors"
	"net/http"

	"github.com/go-chi/render"
)

type SessionResponse struct {
	*Session
	WorkspacePath string `json:"workspace_path,omitempty"`
}

func NewSessionResponse(session *Session) *SessionResponse {
	return &SessionResponse{Session: session}
}

type ReviveResult struct {
	Session       *Session
	WorkspacePath string
}

func NewReviveResponse(result *ReviveResult) *SessionResponse {
	return &SessionResponse{
		Session:       result.Session,
		WorkspacePath: result.WorkspacePath,
	}
}

func (sr *SessionResponse) Render(w http.ResponseWriter, r *http.Request) error {

	return nil
}

type SessionListResponse struct {
	Sessions []Session `json:"sessions"`
}

func NewSessionListResponse(sessions []Session) *SessionListResponse {
	return &SessionListResponse{Sessions: sessions}
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
	*Session
}

func (c *CreateSessionRequest) Bind(r *http.Request) error {

	if c.Session == nil {
		return errors.New("session cannot be nil")
	}

	return ValidateSession(c.Session)
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
