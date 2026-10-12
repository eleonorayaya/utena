package session

import (
	"errors"
	"slices"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

var ErrBlockerNotFound = common.NewNotFound("blocker not found")

type BlockerKind string

const (
	BlockerKindReview BlockerKind = "review"
	BlockerKindSlack  BlockerKind = "slack"
	BlockerKindOther  BlockerKind = "other"
)

var BlockerKinds = []BlockerKind{BlockerKindReview, BlockerKindSlack, BlockerKindOther}

func (k BlockerKind) Valid() bool {
	return slices.Contains(BlockerKinds, k)
}

type SessionBlocker struct {
	gorm.Model
	SessionID  uint        `json:"session_id" gorm:"index;not null"`
	Kind       BlockerKind `json:"kind" gorm:"not null"`
	Note       string      `json:"note,omitempty" gorm:"type:text"`
	URL        string      `json:"url,omitempty" gorm:"type:text"`
	ResolvedAt *time.Time  `json:"resolved_at,omitempty"`
}

func (b SessionBlocker) IsOpen() bool {
	return b.ResolvedAt == nil
}

func (b SessionBlocker) Label() string {
	if b.Note != "" {
		return b.Note
	}
	if b.URL != "" {
		return b.URL
	}
	return string(b.Kind)
}

type SessionBlockerStore struct {
	db db.Database
}

func NewSessionBlockerStore(database db.Database) *SessionBlockerStore {
	return &SessionBlockerStore{db: database}
}

func (s *SessionBlockerStore) Create(b *SessionBlocker) error {
	return s.db.Create(b).Error
}

func (s *SessionBlockerStore) GetByID(id uint) (*SessionBlocker, error) {
	var b SessionBlocker
	if err := s.db.First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBlockerNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (s *SessionBlockerStore) Update(b *SessionBlocker) error {
	return s.db.Save(b).Error
}

func (s *SessionBlockerStore) Delete(id uint) error {
	return s.db.Where("id = ?", id).Unscoped().Delete(&SessionBlocker{}).Error
}
