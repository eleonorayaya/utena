package session

import (
	"github.com/eleonorayaya/utena/internal/db"
)

type DismissedPRStore struct {
	db db.Database
}

func NewDismissedPRStore(database db.Database) *DismissedPRStore {
	return &DismissedPRStore{db: database}
}

func (s *DismissedPRStore) Add(d *DismissedPR) error {
	return s.db.Create(d).Error
}

func (s *DismissedPRStore) IsDismissed(prID uint) bool {
	var d DismissedPR
	result := s.db.Where("pull_request_id = ?", prID).First(&d)
	return result.Error == nil
}

func (s *DismissedPRStore) Delete(prID uint) error {
	return s.db.Where("pull_request_id = ?", prID).Delete(&DismissedPR{}).Error
}
