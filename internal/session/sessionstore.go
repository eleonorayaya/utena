package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/workspace"
	"gorm.io/gorm"
)

type SessionStore struct {
	db db.Database
}

func NewSessionStore(database db.Database) *SessionStore {
	return &SessionStore{
		db: database,
	}
}

func (s *SessionStore) loaded() *gorm.DB {
	return s.db.
		Joins("TmuxSession").
		Preload("ClaudeSessions").
		Preload("SessionActions").
		Preload("Worktrees", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_worktrees.position ASC")
		}).
		Preload("Worktrees.Worktree").
		Preload("Worktrees.Worktree.Branch")
}

func (s *SessionStore) enrichWorkspaces(sessions []*Session) error {
	idSet := make(map[uint]struct{})
	for _, sess := range sessions {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.WorkspaceID == nil || *wt.WorkspaceID == 0 {
				continue
			}
			idSet[*wt.WorkspaceID] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return nil
	}
	ids := make([]uint, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	var workspaces []workspace.Workspace
	if err := s.db.Where("id IN ?", ids).Find(&workspaces).Error; err != nil {
		return err
	}
	byID := make(map[uint]*workspace.Workspace, len(workspaces))
	for i := range workspaces {
		byID[workspaces[i].ID] = &workspaces[i]
	}
	for _, sess := range sessions {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.WorkspaceID == nil {
				continue
			}
			if ws, ok := byID[*wt.WorkspaceID]; ok {
				sess.Worktrees[i].Workspace = ws
			}
		}
	}
	return nil
}

func (s *SessionStore) GetByID(id uint) (*Session, error) {
	var session Session
	if err := s.loaded().First(&session, "sessions.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := s.enrichWorkspaces([]*Session{&session}); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) List() ([]Session, error) {
	var sessions []Session
	if err := s.loaded().Order("sessions.last_used_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	ptrs := make([]*Session, len(sessions))
	for i := range sessions {
		ptrs[i] = &sessions[i]
	}
	if err := s.enrichWorkspaces(ptrs); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) ([]Session, error) {
	var sessions []Session
	if err := s.loaded().
		Where("sessions.id IN (SELECT swt.session_id FROM session_worktrees swt JOIN worktrees wt ON swt.worktree_id = wt.id WHERE wt.workspace_id = ? AND swt.deleted_at IS NULL AND wt.deleted_at IS NULL)", workspaceID).
		Order("sessions.last_used_at DESC").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	ptrs := make([]*Session, len(sessions))
	for i := range sessions {
		ptrs[i] = &sessions[i]
	}
	if err := s.enrichWorkspaces(ptrs); err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("ClaudeSessions", "TmuxSession").Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(sessionConflictMessage(session), ErrSessionAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *SessionStore) Update(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	if session.ID == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	if err := s.db.Omit("ClaudeSessions", "TmuxSession").Save(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(sessionConflictMessage(session), ErrSessionAlreadyExists)
		}
		return err
	}
	return nil
}

// sessionConflictMessage produces a friendly message for a unique-constraint
// violation on the Session table. The only user-visible unique index is on
// TmuxSessionID (one Session per tmux record).
func sessionConflictMessage(session *Session) string {
	if session.TmuxSessionID != nil {
		return fmt.Sprintf("session %q already claims this tmux record — kill the existing session first", session.Name)
	}
	return fmt.Sprintf("session %q already exists", session.Name)
}

func (s *SessionStore) ClearAttachedExcept(id uint) error {
	return s.db.Model(&Session{}).Where("is_attached = ? AND id != ?", true, id).Update("is_attached", false).Error
}

func (s *SessionStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	return s.db.Delete(&Session{}, "id = ?", id).Error
}

func (s *SessionStore) GetByWorkspaceAndName(workspaceID uint, name string, excludeStatuses ...SessionStatus) (*Session, error) {
	var session Session
	q := s.db.Where(
		"name = ? AND id IN (SELECT swt.session_id FROM session_worktrees swt JOIN worktrees wt ON swt.worktree_id = wt.id WHERE wt.workspace_id = ? AND swt.deleted_at IS NULL AND wt.deleted_at IS NULL)",
		name, workspaceID,
	)
	if len(excludeStatuses) > 0 {
		q = q.Where("status NOT IN ?", excludeStatuses)
	}
	if err := q.First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) GetByBranchID(branchID uint) (*Session, error) {
	var session Session
	if err := s.loaded().First(&session,
		"sessions.id IN (SELECT swt.session_id FROM session_worktrees swt JOIN worktrees wt ON swt.worktree_id = wt.id WHERE wt.branch_id = ? AND swt.deleted_at IS NULL AND wt.deleted_at IS NULL)",
		branchID,
	).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := s.enrichWorkspaces([]*Session{&session}); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) GetByTmuxSessionID(tmuxSessionID uint) (*Session, error) {
	var session Session
	if err := s.loaded().First(&session, "sessions.tmux_session_id = ?", tmuxSessionID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := s.enrichWorkspaces([]*Session{&session}); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}
