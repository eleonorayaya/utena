package browser

import (
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var ErrWindowNotFound = common.NewNotFound("browser window not found")

type WindowState string

const (
	WindowOpen    WindowState = "open"
	WindowMissing WindowState = "missing"
)

type BrowserWindow struct {
	gorm.Model
	SessionID      uint        `json:"session_id" gorm:"index"`
	State          WindowState `json:"state"`
	DriverWindowID string      `json:"driver_window_id" gorm:"index"`
	LastFocusedAt  time.Time   `json:"last_focused_at"`
}

type BrowserState struct {
	ID    uint `gorm:"primaryKey"`
	RunID string
}
