package session

import (
	"fmt"
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
	TodoID         *uint                  `json:"todo_id,omitempty" gorm:"index"`
	Status         SessionStatus          `json:"status"`
	IsAttached     bool                   `json:"is_attached"`
	LastUsedAt     time.Time              `json:"last_used_at"`
	SessionRoot    string                 `json:"session_root,omitempty" gorm:"index"`
	ClaudeSessions []claude.ClaudeSession `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	SessionActions []SessionAction        `json:"session_actions,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	Worktrees      []SessionWorktree      `json:"worktrees,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
	TmuxSessionID  *uint                  `json:"tmux_session_id,omitempty" gorm:"uniqueIndex"`
	StatusError    string                 `json:"status_error,omitempty"`
	TmuxSession    *utmux.TmuxSession     `json:"tmux_session,omitempty" gorm:"foreignKey:TmuxSessionID"`
}

type SessionWorktree struct {
	gorm.Model
	SessionID  uint                 `json:"session_id" gorm:"uniqueIndex:idx_session_worktree;index;not null"`
	WorktreeID uint                 `json:"worktree_id" gorm:"uniqueIndex:idx_session_worktree;index;not null"`
	Position   int                  `json:"position"`
	Worktree   *git.Worktree        `json:"worktree,omitempty" gorm:"foreignKey:WorktreeID;constraint:OnDelete:RESTRICT"`
	Workspace  *workspace.Workspace `json:"workspace,omitempty" gorm:"-"`
}

func (s *Session) IsCreating() bool {
	return s.Status == StatusCreating
}

func (s *Session) IsMulti() bool {
	return len(s.Worktrees) > 1
}

// WorkspaceDisplay returns the human-readable label naming the workspace(s)
// this session involves: a single workspace name, "N workspaces · a, b, ..."
// for multi (truncating beyond three names), or "no workspace".
func (s *Session) WorkspaceDisplay() string {
	names := make([]string, 0, len(s.Worktrees))
	for i := range s.Worktrees {
		ws := s.Worktrees[i].Workspace
		if ws != nil && ws.Name != "" {
			names = append(names, ws.Name)
		}
	}
	switch len(names) {
	case 0:
		return "no workspace"
	case 1:
		return names[0]
	case 2, 3:
		return fmt.Sprintf("%d workspaces · %s", len(names), strings.Join(names, ", "))
	default:
		return fmt.Sprintf("%d workspaces · %s, …", len(names), strings.Join(names[:3], ", "))
	}
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
