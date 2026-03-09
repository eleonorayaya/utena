package session

import (
	"errors"
	"strings"
	"time"
)

var ErrSessionAlreadyExists = errors.New("session already exists")
var ErrSessionNotFound = errors.New("session not found")
var ErrSessionAttached = errors.New("cannot delete attached session")
var ErrSessionNotBroken = errors.New("session is not broken")
var ErrCannotActivate = errors.New("cannot activate session in current state")

type SessionStatus string

const (
	StatusCreating SessionStatus = "creating"
	StatusReady    SessionStatus = "ready"
	StatusBroken   SessionStatus = "broken"
	StatusDeleted  SessionStatus = "deleted"
)

type ResourceStatus string

const (
	ResourcePending  ResourceStatus = "pending"
	ResourceCreating ResourceStatus = "creating"
	ResourceReady    ResourceStatus = "ready"
	ResourceFailed   ResourceStatus = "failed"
	ResourceRemoved  ResourceStatus = "removed"
)

type ResourceState struct {
	Status ResourceStatus `json:"status"`
	Error  string         `json:"error,omitempty"`
}

type Resources struct {
	Branch   *ResourceState `json:"branch,omitempty"`
	Worktree *ResourceState `json:"worktree,omitempty"`
	Tmux     *ResourceState `json:"tmux,omitempty"`
}

type Session struct {
	ID              string        `json:"id"`
	TmuxSessionName string        `json:"tmux_session_name,omitempty"`
	Name            string        `json:"name,omitempty"`
	WorkspaceID     string        `json:"workspace_id"`
	Branch          string        `json:"branch,omitempty"`
	BaseBranch      string        `json:"base_branch,omitempty"`
	BranchCreated   bool          `json:"branch_created,omitempty"`
	WorktreePath    string        `json:"worktree_path,omitempty"`
	Status          SessionStatus `json:"status"`
	Resources       *Resources    `json:"resources,omitempty"`
	IsAttached      bool          `json:"is_attached"`
	WorkspaceName   string        `json:"workspace_name,omitempty"`
	LastUsedAt      time.Time     `json:"last_used_at"`
}

func (r *Resources) AllReady() bool {
	if r == nil {
		return true
	}
	for _, rs := range []*ResourceState{r.Branch, r.Worktree, r.Tmux} {
		if rs != nil && rs.Status != ResourceReady {
			return false
		}
	}
	return true
}

func (r *Resources) FirstError() string {
	if r == nil {
		return ""
	}
	for _, rs := range []*ResourceState{r.Branch, r.Worktree, r.Tmux} {
		if rs != nil && rs.Error != "" {
			return rs.Error
		}
	}
	return ""
}

func SanitizeID(name string) string {
	r := strings.NewReplacer(
		" ", "-",
		".", "_",
		"/", "-",
	)
	return strings.ToLower(r.Replace(name))
}

func BuildSessionID(workspaceName, name string) string {
	return SanitizeID(workspaceName + "-" + name)
}
