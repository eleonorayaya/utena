package session

import (
	"time"

	"gorm.io/gorm"
)

type DismissedPR struct {
	gorm.Model
	PullRequestID uint `gorm:"uniqueIndex"`
	DismissedAt   time.Time
}
