package tmux

import (
	"errors"
	"fmt"

	"github.com/eleonorayaya/utena/internal/common"
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
}

func (ts *TmuxSession) Signals() []common.Signal {
	if !ts.IsAlive {
		return []common.Signal{
			{
				Source:   "tmux",
				Key:      fmt.Sprintf("tmux:%d", ts.ID),
				Severity: common.SeverityInfo,
				Label:    "tmux stopped",
			},
		}
	}
	return nil
}
