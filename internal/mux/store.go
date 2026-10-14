package mux

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type Store struct {
	db db.Database
}

func NewStore(database db.Database) *Store {
	return &Store{
		db: database,
	}
}

func (s *Store) Add(session *Session) error {
	if session == nil {
		return errors.New("mux session cannot be nil")
	}

	if err := s.db.Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(
				fmt.Sprintf("mux session %q already exists — pick a different name or kill the existing one", session.Name),
				ErrSessionAlreadyExists,
			)
		}
		return err
	}
	return nil
}

func (s *Store) GetByID(id uint) (*Session, error) {
	var session Session
	if err := s.db.First(&session, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *Store) GetByName(name string) (*Session, error) {
	var session Session
	if err := s.db.First(&session, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *Store) List() []Session {
	var sessions []Session
	s.db.Find(&sessions)
	return sessions
}

func (s *Store) Update(session *Session) error {
	if session == nil {
		return errors.New("mux session cannot be nil")
	}
	if session.ID == 0 {
		return errors.New("mux session ID cannot be zero")
	}

	var existing Session
	if err := s.db.First(&existing, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionNotFound
		}
		return err
	}

	if err := s.db.Save(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.WrapConflict(
				fmt.Sprintf("mux session %q already exists — pick a different name or kill the existing one", session.Name),
				ErrSessionAlreadyExists,
			)
		}
		return err
	}
	return nil
}

// BackfillStatus migrates legacy rows: any Session that has no Status set
// (older schema where IsAlive bool was the source of truth) is updated using
// the orphan is_alive column when present. Safe to run on fresh tables (the
// is_alive column may not exist; we tolerate the error).
func (s *Store) BackfillStatus() error {
	hasIsAlive := false
	if err := s.db.Exec("SELECT is_alive FROM mux_sessions LIMIT 1").Error; err == nil {
		hasIsAlive = true
	}
	if hasIsAlive {
		if err := s.db.Exec(
			"UPDATE mux_sessions SET status = CASE WHEN is_alive THEN ? ELSE ? END WHERE status IS NULL OR status = ''",
			string(StatusActive), string(StatusInactive),
		).Error; err != nil {
			return err
		}
		return nil
	}
	return s.db.Exec(
		"UPDATE mux_sessions SET status = ? WHERE status IS NULL OR status = ''",
		string(StatusInactive),
	).Error
}

func (s *Store) Delete(id uint) error {
	if id == 0 {
		return errors.New("mux session ID cannot be zero")
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
