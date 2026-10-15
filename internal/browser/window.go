package browser

import (
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var ErrWindowNotFound = common.NewNotFound("browser window not found")

type WindowState string

const (
	WindowOpen        WindowState = "open"
	WindowMissing     WindowState = "missing"
	WindowSnapshotted WindowState = "snapshotted"
)

type SavedTab struct {
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
}

type BrowserWindow struct {
	gorm.Model
	SessionID      uint        `json:"session_id" gorm:"index"`
	State          WindowState `json:"state"`
	DriverWindowID string      `json:"driver_window_id" gorm:"index"`
	LastFocusedAt  time.Time   `json:"last_focused_at"`
	Tabs           []SavedTab  `json:"tabs" gorm:"serializer:json"`
	SnapshotAt     time.Time   `json:"snapshot_at"`
}

type BrowserState struct {
	ID    uint `gorm:"primaryKey"`
	RunID string
}

func savedTabs(w Window) []SavedTab {
	var out []SavedTab
	for _, t := range w.Tabs {
		if !isWebURL(t.URL) {
			continue
		}
		out = append(out, SavedTab{URL: t.URL, Title: t.Title, Active: t.Active, Pinned: t.Pinned})
	}
	return out
}

func isWebURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}
