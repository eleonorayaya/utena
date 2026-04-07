package session

import (
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"gorm.io/gorm"
)

var ErrSessionAlreadyExists = common.NewConflict("session already exists")
var ErrSessionNotFound = common.NewNotFound("session not found")
var ErrSessionAttached = common.NewInvalidRequest("cannot delete attached session")
var ErrSessionNotBroken = common.NewInvalidRequest("session is not broken")
var ErrCannotActivate = common.NewInvalidRequest("cannot activate session in current state")

type SessionStatus string

const (
	StatusCreating  SessionStatus = "creating"
	StatusActive    SessionStatus = "active"
	StatusBroken    SessionStatus = "broken"
	StatusDeleted   SessionStatus = "deleted"
	StatusPending   SessionStatus = "pending"
	StatusInactive  SessionStatus = "inactive"
	StatusArchived  SessionStatus = "archived"
	StatusCompleted SessionStatus = "completed"
)

type Session struct {
	gorm.Model
	Name           string                 `json:"name,omitempty"`
	WorkspaceID    uint                   `json:"workspace_id" gorm:"index"`
	TodoID         *uint                  `json:"todo_id,omitempty" gorm:"index"`
	Status         SessionStatus          `json:"status"`
	IsAttached     bool                   `json:"is_attached"`
	LastUsedAt     time.Time              `json:"last_used_at"`
	Workspace      *workspace.Workspace   `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID"`
	ClaudeSessions []claude.ClaudeSession `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID"`
	BranchID       *uint                  `json:"branch_id,omitempty" gorm:"index"`
	TmuxSessionID  *uint                  `json:"tmux_session_id,omitempty" gorm:"uniqueIndex"`
	StatusError    string                 `json:"status_error,omitempty"`
	GitBranch      *git.Branch            `json:"git_branch,omitempty" gorm:"foreignKey:BranchID"`
	TmuxSession    *utmux.TmuxSession     `json:"tmux_session,omitempty" gorm:"foreignKey:TmuxSessionID"`
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
