package tmux

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type TmuxStore struct {
	db db.Database
}

func NewTmuxStore(database db.Database) *TmuxStore {
	return &TmuxStore{
		db: database,
	}
}

func (s *TmuxStore) Add(session *TmuxSession) error {
	if session == nil {
		return errors.New("tmux session cannot be nil")
	}

	if err := s.db.Create(session).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return fmt.Errorf("tmux session '%s' already exists: %w", session.Name, ErrTmuxSessionAlreadyExists)
		}
		return err
	}
	return nil
}

func (s *TmuxStore) GetByID(id uint) (*TmuxSession, error) {
	var session TmuxSession
	if err := s.db.First(&session, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTmuxSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *TmuxStore) GetByName(name string) (*TmuxSession, error) {
	var session TmuxSession
	if err := s.db.First(&session, "name = ?", name).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTmuxSessionNotFound
		}
		return nil, err
	}
	return &session, nil
}

func (s *TmuxStore) List() []TmuxSession {
	var sessions []TmuxSession
	s.db.Find(&sessions)
	return sessions
}

func (s *TmuxStore) Update(session *TmuxSession) error {
	if session == nil {
		return errors.New("tmux session cannot be nil")
	}
	if session.ID == 0 {
		return errors.New("tmux session ID cannot be zero")
	}

	var existing TmuxSession
	if err := s.db.First(&existing, "id = ?", session.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTmuxSessionNotFound
		}
		return err
	}

	return s.db.Save(session).Error
}

func (s *TmuxStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("tmux session ID cannot be zero")
	}

	var existing TmuxSession
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTmuxSessionNotFound
		}
		return err
	}

	return s.db.Delete(&TmuxSession{}, "id = ?", id).Error
}
