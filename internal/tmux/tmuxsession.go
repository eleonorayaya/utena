package tmux

import (
	"errors"

	"gorm.io/gorm"
)

var (
	ErrTmuxSessionNotFound      = errors.New("tmux session not found")
	ErrTmuxSessionAlreadyExists = errors.New("tmux session already exists")
)

type TmuxSession struct {
	gorm.Model
	Name     string            `json:"name" gorm:"uniqueIndex"`
	StartDir string            `json:"start_dir"`
	Env      map[string]string `json:"env" gorm:"serializer:json"`
	IsAlive  bool              `json:"is_alive"`
	Windows  []Window          `json:"windows,omitempty" gorm:"-"`
}
