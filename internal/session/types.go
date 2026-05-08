package session

import (
	"errors"
	"net/http"
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

type CreateSessionRequest struct {
	Name         string `json:"name,omitempty"`
	WorkspaceIDs []uint `json:"workspace_ids,omitempty"`
	Branch       string `json:"branch,omitempty"`
	BaseBranch   string `json:"base_branch,omitempty"`
	TodoID       *uint  `json:"todo_id,omitempty"`
}

func (c *CreateSessionRequest) Bind(r *http.Request) error {
	if len(c.WorkspaceIDs) == 0 {
		return errors.New("workspace_ids is required")
	}
	if c.Name != "" {
		return ValidateSessionName(c.Name)
	}
	return nil
}

func (c *CreateSessionRequest) IsMulti() bool {
	return len(c.WorkspaceIDs) >= 2
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
