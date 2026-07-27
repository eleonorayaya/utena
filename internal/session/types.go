package session

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/eleonorayaya/utena/internal/claude"
)

type SessionResponse struct {
	*Session
	AttentionStatus claude.ClaudeSessionStatus `json:"attention_status,omitempty"`
}

func NewSessionResponse(session *Session) *SessionResponse {
	return &SessionResponse{
		Session:         session,
		AttentionStatus: session.AttentionStatus(),
	}
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

const notificationTypePullRequest = "pull_request"

type prNotification struct {
	Number        int    `json:"number"`
	Title         string `json:"title"`
	State         string `json:"state"`
	PreviousState string `json:"previous_state,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Checks        string `json:"checks,omitempty"`
	URL           string `json:"url"`
}

type WorkspaceBranchSpec struct {
	WorkspaceID uint   `json:"workspace_id"`
	Branch      string `json:"branch,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
}

type CreateSessionRequest struct {
	Name       string                `json:"name,omitempty"`
	Workspaces []WorkspaceBranchSpec `json:"workspaces"`
	TodoID     *uint                 `json:"todo_id,omitempty"`
}

func (c *CreateSessionRequest) Bind(r *http.Request) error {
	if len(c.Workspaces) == 0 {
		return errors.New("workspaces is required")
	}
	seen := make(map[uint]struct{}, len(c.Workspaces))
	hasBase := false
	for i, ws := range c.Workspaces {
		if ws.WorkspaceID == 0 {
			return fmt.Errorf("workspaces[%d].workspace_id is required", i)
		}
		if _, dup := seen[ws.WorkspaceID]; dup {
			return fmt.Errorf("workspace %d listed more than once", ws.WorkspaceID)
		}
		seen[ws.WorkspaceID] = struct{}{}
		hasBranch := ws.Branch != ""
		hasBaseBranch := ws.BaseBranch != ""
		if hasBranch == hasBaseBranch {
			return fmt.Errorf("workspaces[%d]: exactly one of branch or base_branch is required", i)
		}
		if hasBaseBranch {
			hasBase = true
		}
	}
	if hasBase && c.Name == "" {
		return errors.New("name is required when any workspace uses base_branch")
	}
	if c.Name != "" {
		return ValidateSessionName(c.Name)
	}
	return nil
}

type AddWorkspaceRequest struct {
	WorkspaceBranchSpec
}

func (r *AddWorkspaceRequest) Bind(_ *http.Request) error {
	if r.WorkspaceID == 0 {
		return errors.New("workspace_id is required")
	}
	if (r.Branch != "") == (r.BaseBranch != "") {
		return errors.New("exactly one of branch or base_branch is required")
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
