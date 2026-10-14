package mux

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var (
	ErrSessionNotFound      = errors.New("mux session not found")
	ErrSessionAlreadyExists = common.NewConflict("mux session already exists")
)

type SessionStatus string

const (
	StatusPending  SessionStatus = "pending"
	StatusActive   SessionStatus = "active"
	StatusInactive SessionStatus = "inactive"
)

type Session struct {
	gorm.Model
	Name     string            `json:"name" gorm:"uniqueIndex"`
	StartDir string            `json:"start_dir"`
	Env      map[string]string `json:"env" gorm:"serializer:json"`
	Status   SessionStatus     `json:"status" gorm:"index"`
	Windows  []Window          `json:"windows,omitempty" gorm:"-"`
}

func (t *Session) GetStatus() SessionStatus  { return t.Status }
func (t *Session) SetStatus(s SessionStatus) { t.Status = s }

func (Session) TableName() string { return "mux_sessions" }
