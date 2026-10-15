package browser

import (
	"errors"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type Store struct {
	db db.Database
}

func NewStore(database db.Database) *Store {
	return &Store{db: database}
}

func (s *Store) Add(w *BrowserWindow) error {
	return s.db.Create(w).Error
}

func (s *Store) GetByDriverWindowID(id string) (*BrowserWindow, error) {
	if id == "" {
		return nil, ErrWindowNotFound
	}
	var w BrowserWindow
	if err := s.db.Where("driver_window_id = ? AND state = ?", id, WindowOpen).First(&w).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWindowNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *Store) ListOpenBySession(sessionID uint) ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("session_id = ? AND state = ?", sessionID, WindowOpen).
		Order("last_focused_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (s *Store) ListOpen() ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("state = ?", WindowOpen).Order("id").Find(&rows).Error
	return rows, err
}

func (s *Store) OwnedDriverWindowIDs() (map[string]bool, error) {
	var rows []BrowserWindow
	if err := s.db.Where("state = ?", WindowOpen).Find(&rows).Error; err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(rows))
	for _, r := range rows {
		owned[r.DriverWindowID] = true
	}
	return owned, nil
}

func (s *Store) DeleteByDriverWindowID(id string) error {
	if id == "" {
		return nil
	}
	return s.db.Where("driver_window_id = ? AND state = ?", id, WindowOpen).Delete(&BrowserWindow{}).Error
}

func (s *Store) TouchFocused(id string, at time.Time) error {
	if id == "" {
		return nil
	}
	return s.db.Model(&BrowserWindow{}).
		Where("driver_window_id = ? AND state = ?", id, WindowOpen).
		Update("last_focused_at", at).Error
}

func (s *Store) MarkMissingExcept(live map[string]bool) error {
	var rows []BrowserWindow
	if err := s.db.Where("state = ?", WindowOpen).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if live[r.DriverWindowID] {
			continue
		}
		if err := s.db.Model(&BrowserWindow{}).Where("id = ?", r.ID).
			Updates(map[string]any{"state": WindowMissing, "driver_window_id": ""}).Error; err != nil {
			return err
		}
	}
	return nil
}

const browserStateID = 1

func (s *Store) RunID() (string, error) {
	var st BrowserState
	err := s.db.Where("id = ?", browserStateID).First(&st).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	return st.RunID, err
}

func (s *Store) SetRunID(run string) error {
	return s.db.Save(&BrowserState{ID: browserStateID, RunID: run}).Error
}
