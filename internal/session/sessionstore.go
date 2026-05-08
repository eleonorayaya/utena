package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
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
		Joins("GitBranch").
		Joins("TmuxSession").
		Preload("ClaudeSessions").
		Preload("SessionActions").
		Preload("Workspaces", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_workspaces.position ASC")
		}).
		Preload("Workspaces.Workspace").
		Preload("Workspaces.GitBranch").
		Preload("Worktrees", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_worktrees.position ASC")
		}).
		Preload("Worktrees.Worktree").
		Preload("Worktrees.Worktree.Branch")
}

func (s *SessionStore) GetByID(id uint) (*Session, error) {
	var session Session
	if err := s.loaded().First(&session, "sessions.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) List() ([]Session, error) {
	var sessions []Session
	if err := s.loaded().Order("sessions.last_used_at DESC").Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) ([]Session, error) {
	var sessions []Session
	if err := s.loaded().
		Where("sessions.id IN (SELECT session_id FROM session_workspaces WHERE workspace_id = ? AND deleted_at IS NULL)", workspaceID).
		Order("sessions.last_used_at DESC").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("ClaudeSessions", "GitBranch", "TmuxSession", "Workspaces").Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("session '%s' already exists: %w", session.Name, ErrSessionAlreadyExists)
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

	return s.db.Omit("ClaudeSessions", "GitBranch", "TmuxSession", "Workspaces").Save(session).Error
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
	q := s.db.Where("name = ? AND id IN (SELECT session_id FROM session_workspaces WHERE workspace_id = ? AND deleted_at IS NULL)", name, workspaceID)
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
	if err := s.loaded().First(&session, "sessions.id IN (SELECT session_id FROM session_workspaces WHERE branch_id = ? AND deleted_at IS NULL)", branchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
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
	return &session, nil
}

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}
