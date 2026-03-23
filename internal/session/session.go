package session

import (
	"errors"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/workspace"
	"gorm.io/gorm"
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
	gorm.Model
	TmuxSessionName string                 `json:"tmux_session_name,omitempty" gorm:"uniqueIndex"`
	Name            string                 `json:"name,omitempty"`
	WorkspaceID     uint                   `json:"workspace_id" gorm:"index"`
	TodoID          *uint                  `json:"todo_id,omitempty" gorm:"index"`
	Branch          string                 `json:"branch,omitempty"`
	BaseBranch      string                 `json:"base_branch,omitempty"`
	WorktreePath    string                 `json:"worktree_path,omitempty"`
	Status          SessionStatus          `json:"status"`
	Resources       *Resources             `json:"resources,omitempty" gorm:"serializer:json"`
	IsAttached      bool                   `json:"is_attached"`
	LastUsedAt      time.Time              `json:"last_used_at"`
	Workspace       *workspace.Workspace   `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID"`
	ClaudeSessions  []claude.ClaudeSession `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID"`
}

func SanitizeTmuxName(name string) string {
	r := strings.NewReplacer(
		" ", "-",
		".", "_",
		"/", "-",
	)
	return strings.ToLower(r.Replace(name))
}

func BuildTmuxSessionName(workspaceName, name string) string {
	return SanitizeTmuxName(workspaceName + "-" + name)
}
