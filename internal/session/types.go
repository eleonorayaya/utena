package session

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/mux"
)

type SessionResponse struct {
	*Session
	AttentionStatus claude.ClaudeSessionStatus `json:"attention_status,omitempty"`
	IsBlocked       bool                       `json:"is_blocked"`
}

func NewSessionResponse(session *Session) *SessionResponse {
	return &SessionResponse{
		Session:         session,
		AttentionStatus: session.AttentionStatus(),
		IsBlocked:       session.IsBlocked(),
	}
}

func (sr *SessionResponse) Render(w http.ResponseWriter, r *http.Request) error {
	return nil
}

const ResourceStatusUnmanaged = "unmanaged"

type SessionResources struct {
	SessionID  *uint              `json:"session_id,omitempty"`
	Name       string             `json:"name"`
	MuxSession string             `json:"mux_session"`
	Status     string             `json:"status"`
	CPUPercent float64            `json:"cpu_pct"`
	RSSBytes   int64              `json:"rss_bytes"`
	Procs      int                `json:"procs"`
	Processes  []mux.ProcessUsage `json:"processes"`
}

type SessionResourcesResponse struct {
	Sessions  []SessionResources `json:"sessions"`
	SampledAt time.Time          `json:"sampled_at"`
}

func (r *SessionResourcesResponse) Render(w http.ResponseWriter, req *http.Request) error {
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

type WorkspaceBranchSpec struct {
	WorkspaceID uint   `json:"workspace_id"`
	Branch      string `json:"branch,omitempty"`
	BaseBranch  string `json:"base_branch,omitempty"`
}

type CreateSessionRequest struct {
	Name       string                `json:"name,omitempty"`
	Workspaces []WorkspaceBranchSpec `json:"workspaces"`
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

type AddBlockerRequest struct {
	Kind BlockerKind `json:"kind"`
	Note string      `json:"note,omitempty"`
	URL  string      `json:"url,omitempty"`
}

func (r *AddBlockerRequest) Bind(_ *http.Request) error {
	if !r.Kind.Valid() {
		return fmt.Errorf("kind must be one of review, slack, other (got %q)", r.Kind)
	}
	return nil
}

type BranchHookRequest struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

func (r *BranchHookRequest) Bind(_ *http.Request) error {
	if r.Path == "" {
		return errors.New("path is required")
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
