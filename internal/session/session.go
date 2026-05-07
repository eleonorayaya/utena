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
	SessionRoot    string                 `json:"session_root,omitempty" gorm:"index"`
	Workspace      *workspace.Workspace   `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID"`
	ClaudeSessions []claude.ClaudeSession `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	SessionActions []SessionAction        `json:"session_actions,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	Workspaces     []SessionWorkspace     `json:"workspaces,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	BranchID       *uint                  `json:"branch_id,omitempty" gorm:"index"`
	TmuxSessionID  *uint                  `json:"tmux_session_id,omitempty" gorm:"uniqueIndex"`
	StatusError    string                 `json:"status_error,omitempty"`
	GitBranch      *git.Branch            `json:"git_branch,omitempty" gorm:"foreignKey:BranchID"`
	TmuxSession    *utmux.TmuxSession     `json:"tmux_session,omitempty" gorm:"foreignKey:TmuxSessionID"`
}

type SessionWorkspace struct {
	gorm.Model
	SessionID    uint                 `json:"session_id" gorm:"uniqueIndex:idx_session_workspace;index;not null"`
	WorkspaceID  uint                 `json:"workspace_id" gorm:"uniqueIndex:idx_session_workspace;index;not null"`
	BranchID     *uint                `json:"branch_id,omitempty" gorm:"index"`
	WorktreePath string               `json:"worktree_path,omitempty"`
	Position     int                  `json:"position"`
	Workspace    *workspace.Workspace `json:"workspace,omitempty" gorm:"foreignKey:WorkspaceID;constraint:OnDelete:RESTRICT"`
	GitBranch    *git.Branch          `json:"git_branch,omitempty" gorm:"foreignKey:BranchID"`
}

func (s *Session) IsCreating() bool {
	return s.Status == StatusCreating
}

func (s *Session) IsMulti() bool {
	return len(s.Workspaces) > 1
}

// WorkspaceNames returns the names of every workspace this session involves,
// in stable display order. Falls back to the legacy Session.Workspace pointer
// for sessions that haven't run through OnAppStart's backfill yet.
func (s *Session) WorkspaceNames() []string {
	if len(s.Workspaces) > 0 {
		names := make([]string, 0, len(s.Workspaces))
		for i := range s.Workspaces {
			if s.Workspaces[i].Workspace != nil && s.Workspaces[i].Workspace.Name != "" {
				names = append(names, s.Workspaces[i].Workspace.Name)
			}
		}
		return names
	}
	if s.Workspace != nil && s.Workspace.Name != "" {
		return []string{s.Workspace.Name}
	}
	return nil
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
