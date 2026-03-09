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
