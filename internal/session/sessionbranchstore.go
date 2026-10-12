package session

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm/clause"
)

type SessionBranchStore struct {
	db db.Database
}

func NewSessionBranchStore(database db.Database) *SessionBranchStore {
	return &SessionBranchStore{db: database}
}

func (s *SessionBranchStore) Add(sb *SessionBranch) error {
	if sb == nil {
		return errors.New("session branch cannot be nil")
	}
	return s.db.Omit("Branch").Clauses(clause.OnConflict{DoNothing: true}).Create(sb).Error
}
