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

func (s *Store) Get(id uint) (*BrowserWindow, error) {
	var w BrowserWindow
	if err := s.db.First(&w, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWindowNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *Store) GetByDriverWindowID(id string) (*BrowserWindow, error) {
	if id == "" {
		return nil, ErrWindowNotFound
	}
	var w BrowserWindow
	if err := s.openWindow(id).First(&w).Error; err != nil {
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
	return s.openWindow(id).Delete(&BrowserWindow{}).Error
}

func (s *Store) TouchFocused(id string, at time.Time) error {
	if id == "" {
		return nil
	}
	return s.openWindow(id).Model(&BrowserWindow{}).Update("last_focused_at", at).Error
}

func (s *Store) MarkMissingExcept(live map[string]bool) error {
	q := s.db.Model(&BrowserWindow{}).Where("state = ?", WindowOpen)
	if len(live) > 0 {
		ids := make([]string, 0, len(live))
		for id := range live {
			ids = append(ids, id)
		}
		q = q.Where("driver_window_id NOT IN ?", ids)
	}
	return q.Updates(missingUpdate()).Error
}

func missingUpdate() map[string]any {
	return map[string]any{"state": WindowMissing, "driver_window_id": ""}
}

func (s *Store) openWindow(id string) *gorm.DB {
	return s.db.Where("driver_window_id = ? AND state = ?", id, WindowOpen)
}

func (s *Store) SaveTabs(driverWindowID string, tabs []SavedTab, at time.Time) error {
	if driverWindowID == "" {
		return nil
	}
	return s.openWindow(driverWindowID).Model(&BrowserWindow{}).
		Select("Tabs", "SnapshotAt").
		Updates(&BrowserWindow{Tabs: tabs, SnapshotAt: at}).Error
}

func (s *Store) SetTabs(id uint, tabs []SavedTab) error {
	return s.db.Model(&BrowserWindow{}).Where("id = ?", id).
		Select("Tabs").
		Updates(&BrowserWindow{Tabs: tabs}).Error
}

func (s *Store) ListAll() ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Order("session_id, id").Find(&rows).Error
	return rows, err
}

func (s *Store) ListRestorable(sessionID uint) ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("session_id = ? AND state IN ?", sessionID, []WindowState{WindowMissing, WindowSnapshotted}).
		Order("last_focused_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (s *Store) MarkSnapshotted(id uint, tabs []SavedTab, at time.Time) error {
	return s.db.Model(&BrowserWindow{}).
		Where("id = ?", id).
		Select("State", "DriverWindowID", "Tabs", "SnapshotAt").
		Updates(&BrowserWindow{State: WindowSnapshotted, Tabs: tabs, SnapshotAt: at}).Error
}

func (s *Store) Reopen(id uint, driverWindowID string, at time.Time) error {
	if driverWindowID == "" {
		return nil
	}
	return s.db.Model(&BrowserWindow{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": WindowOpen, "driver_window_id": driverWindowID, "last_focused_at": at}).Error
}

func (s *Store) MarkMissingByDriverWindowID(id string) error {
	if id == "" {
		return nil
	}
	return s.openWindow(id).Model(&BrowserWindow{}).Updates(missingUpdate()).Error
}

func (s *Store) Delete(id uint) error {
	return s.db.Delete(&BrowserWindow{}, id).Error
}

func (s *Store) DeleteBySession(sessionID uint) error {
	return s.db.Where("session_id = ?", sessionID).Delete(&BrowserWindow{}).Error
}

func (s *Store) ListMissing() ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("state = ?", WindowMissing).Order("id").Find(&rows).Error
	return rows, err
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
