package tmux

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrTmuxSessionNotFound      = errors.New("tmux session not found")
	ErrTmuxSessionAlreadyExists = errors.New("tmux session already exists")
)

type TmuxSessionStatus string

const (
	TmuxStatusPending  TmuxSessionStatus = "pending"
	TmuxStatusActive   TmuxSessionStatus = "active"
	TmuxStatusInactive TmuxSessionStatus = "inactive"
)

type TmuxSession struct {
	gorm.Model
	Name     string            `json:"name" gorm:"uniqueIndex"`
	StartDir string            `json:"start_dir"`
	Env      map[string]string `json:"env" gorm:"serializer:json"`
	Status   TmuxSessionStatus `json:"status" gorm:"index"`
	Windows  []Window          `json:"windows,omitempty" gorm:"-"`
}

func (t *TmuxSession) GetStatus() TmuxSessionStatus  { return t.Status }
func (t *TmuxSession) SetStatus(s TmuxSessionStatus) { t.Status = s }
