package session

import (
	"errors"
	"strings"
	"time"
)

var ErrSessionAlreadyExists = errors.New("session already exists")
var ErrSessionNotFound = errors.New("session not found")
var ErrSessionAttached = errors.New("cannot delete attached session")
var ErrSessionNotDead = errors.New("session is not dead")
var ErrSessionDead = errors.New("cannot activate dead session")

type Session struct {
	ID            string    `json:"id"`
	Name          string    `json:"name,omitempty"`
	WorkspaceID   string    `json:"workspace_id"`
	WorkspaceName string    `json:"workspace_name,omitempty"`
	Branch        string    `json:"branch,omitempty"`
	BaseBranch    string    `json:"base_branch,omitempty"`
	BranchCreated bool      `json:"branch_created,omitempty"`
	WorktreePath  string    `json:"worktree_path,omitempty"`
	IsAttached    bool      `json:"is_attached"`
	IsActive      bool      `json:"is_active"`
	IsDead        bool      `json:"is_dead"`
	IsDeleted     bool      `json:"is_deleted"`
	Cleanup       *Cleanup  `json:"cleanup,omitempty"`
	LastUsedAt    time.Time `json:"last_used_at"`
}

type Cleanup struct {
	ZellijSessionKilled bool `json:"zellij_session_killed"`
	WorktreeRemoved     bool `json:"worktree_removed"`
	BranchDeleted       bool `json:"branch_deleted"`
}

func BuildSessionID(workspaceName, name string) string {
	sanitized := strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
	return sanitized + "-" + name
}

func sanitizeBranchName(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}
