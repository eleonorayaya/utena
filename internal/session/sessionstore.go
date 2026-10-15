package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/workspace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
		Joins("MuxSession").
		Preload("ClaudeSessions").
		Preload("SessionActions").
		Preload("SetupSteps", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_setup_steps.position ASC")
		}).
		Preload("Blockers", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_blockers.created_at ASC")
		}).
		Preload("Worktrees", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_worktrees.position ASC")
		}).
		Preload("Worktrees.Worktree").
		Preload("Worktrees.Worktree.Branch").
		Preload("Branches").
		Preload("Branches.Branch")
}

func (s *SessionStore) enrichWorkspaces(sessions []*Session) error {
	repoSet := make(map[uint]struct{})
	for _, sess := range sessions {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.RepoID == 0 {
				continue
			}
			repoSet[wt.RepoID] = struct{}{}
		}
	}
	if len(repoSet) == 0 {
		return nil
	}
	repoIDs := make([]uint, 0, len(repoSet))
	for id := range repoSet {
		repoIDs = append(repoIDs, id)
	}
	var workspaces []workspace.Workspace
	if err := s.db.Where("repo_id IN ?", repoIDs).Find(&workspaces).Error; err != nil {
		return err
	}
	byRepoID := make(map[uint]*workspace.Workspace, len(workspaces))
	for i := range workspaces {
		if workspaces[i].RepoID != nil {
			byRepoID[*workspaces[i].RepoID] = &workspaces[i]
		}
	}
	for _, sess := range sessions {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil {
				continue
			}
			if ws, ok := byRepoID[wt.RepoID]; ok {
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

func (s *SessionStore) List(statuses ...SessionStatus) ([]Session, error) {
	var sessions []Session
	q := s.loaded()
	if len(statuses) > 0 {
		q = q.Where("sessions.status IN ?", statuses)
	}
	if err := q.Order("sessions.last_used_at DESC").Find(&sessions).Error; err != nil {
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

type MuxSessionRef struct {
	ID      uint
	Name    string
	Status  SessionStatus
	MuxName string
}

func (s *SessionStore) ListByMuxNames(muxNames []string) ([]MuxSessionRef, error) {
	var refs []MuxSessionRef
	if len(muxNames) == 0 {
		return refs, nil
	}
	err := s.db.Model(&Session{}).
		Select("sessions.id, sessions.name, sessions.status, mux_sessions.name AS mux_name").
		Joins("JOIN mux_sessions ON mux_sessions.id = sessions.mux_session_id AND mux_sessions.deleted_at IS NULL").
		Where("sessions.status <> ? AND mux_sessions.name IN ?", StatusDeleted, muxNames).
		Scan(&refs).Error
	return refs, err
}

func (s *SessionStore) ListByWorkspace(workspaceID uint) ([]Session, error) {
	var sessions []Session
	if err := s.loaded().
		Where("sessions.id IN (?)", s.sessionsForWorkspace(workspaceID)).
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

func (s *SessionStore) sessionsForWorkspace(workspaceID uint) *gorm.DB {
	return s.db.
		Model(&SessionWorktree{}).
		Select("session_worktrees.session_id").
		Joins("JOIN worktrees ON worktrees.id = session_worktrees.worktree_id").
		Joins("JOIN workspaces ON workspaces.repo_id = worktrees.repo_id").
		Where("workspaces.id = ?", workspaceID)
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if err := s.db.Omit("ClaudeSessions", "MuxSession").Create(session).Error; err != nil {
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

	if err := s.db.Omit("ClaudeSessions", "MuxSession").Save(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(sessionConflictMessage(session), ErrSessionAlreadyExists)
		}
		return err
	}
	return nil
}

// sessionConflictMessage produces a friendly message for a unique-constraint
// violation on the Session table. The only user-visible unique index is on
// MuxSessionID (one Session per mux record).
func sessionConflictMessage(session *Session) string {
	if session.MuxSessionID != nil {
		return fmt.Sprintf("session %q already claims this mux record — kill the existing session first", session.Name)
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

// HardDelete physically removes the session row (bypassing GORM soft delete) so
// the database's ON DELETE CASCADE constraints fire, cleaning up join rows,
// claude sessions, actions, and setup steps.
func (s *SessionStore) HardDelete(id uint) error {
	if id == 0 {
		return errors.New("session ID cannot be zero")
	}
	return s.db.Where("id = ?", id).Unscoped().Delete(&Session{}).Error
}

func (s *SessionStore) GetByWorkspaceAndName(workspaceID uint, name string, excludeStatuses ...SessionStatus) (*Session, error) {
	var session Session
	q := s.db.Where("name = ? AND id IN (?)", name, s.sessionsForWorkspace(workspaceID))
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
	viaWorktree := s.db.
		Model(&SessionWorktree{}).
		Select("session_worktrees.session_id").
		Joins("JOIN worktrees ON worktrees.id = session_worktrees.worktree_id").
		Where("worktrees.branch_id = ?", branchID)
	viaHistory := s.db.
		Model(&SessionBranch{}).
		Select("session_branches.session_id").
		Where("session_branches.branch_id = ?", branchID)

	var session Session
	err := s.loaded().
		Where("sessions.id IN (?) OR sessions.id IN (?)", viaWorktree, viaHistory).
		Order(clause.OrderBy{Expression: clause.Expr{
			SQL:  "sessions.id IN (?) DESC, sessions.status IN ? ASC, sessions.last_used_at DESC, sessions.id ASC",
			Vars: []any{viaWorktree, unwatchedStatuses},
		}}).
		Take(&session).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.enrichWorkspaces([]*Session{&session}); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) GetByMuxSessionID(muxSessionID uint) (*Session, error) {
	var session Session
	if err := s.loaded().First(&session, "sessions.mux_session_id = ?", muxSessionID).Error; err != nil {
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

func (s *SessionStore) GetStatus(id uint) (SessionStatus, error) {
	var sess Session
	if err := s.db.Model(&Session{}).Select("status").First(&sess, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrSessionNotFound
		}
		return "", err
	}
	return sess.Status, nil
}
