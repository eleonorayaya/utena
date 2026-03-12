package session

import (
	"errors"
	"net/http"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/render"
)

type SessionResponse struct {
	*Session
	WorkspaceName string `json:"workspace_name,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
}

func NewSessionResponse(session *Session, ws *workspace.Workspace) *SessionResponse {
	resp := &SessionResponse{Session: session}
	if ws != nil {
		resp.WorkspaceName = ws.Name
		resp.WorkspacePath = ws.Path
	}
	return resp
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
		list[i] = NewSessionResponse(&s, nil)
	}
	return list
}

type CreateSessionRequest struct {
	Name           string `json:"name,omitempty"`
	WorkspaceID    uint   `json:"workspace_id"`
	Branch         string `json:"branch,omitempty"`
	BaseBranch     string `json:"base_branch,omitempty"`
	BranchCreated  bool   `json:"branch_created"`
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
