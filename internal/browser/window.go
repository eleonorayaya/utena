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

type TabState string

const (
	TabPending TabState = "pending"
	TabOpen    TabState = "open"
	TabSaved   TabState = "saved"
)

type SavedTab struct {
	URL    string   `json:"url"`
	Title  string   `json:"title"`
	Active bool     `json:"active"`
	Pinned bool     `json:"pinned"`
	State  TabState `json:"state,omitempty"`
}

func (t SavedTab) StateIn(window WindowState) TabState {
	switch {
	case t.State == TabPending:
		return TabPending
	case window == WindowOpen:
		return TabOpen
	}
	return TabSaved
}

type BrowserWindow struct {
	gorm.Model
	SessionID      uint        `json:"session_id" gorm:"index"`
	Driver         string      `json:"driver" gorm:"index"`
	State          WindowState `json:"state"`
	DriverWindowID string      `json:"driver_window_id" gorm:"index"`
	LastFocusedAt  time.Time   `json:"last_focused_at"`
	Tabs           []SavedTab  `json:"tabs" gorm:"serializer:json"`
	SnapshotAt     time.Time   `json:"snapshot_at"`
}

type BrowserState struct {
	ID     uint   `gorm:"primaryKey"`
	Driver string `gorm:"uniqueIndex"`
	RunID  string
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

func mergeTabs(w Window, saved []SavedTab) ([]SavedTab, bool) {
	live := savedTabs(w)
	if len(live) == 0 {
		return saved, false
	}
	for _, t := range saved {
		if t.State != TabPending {
			continue
		}
		if _, ok := MatchTab(w.Tabs, t.URL); !ok {
			live = append(live, t)
		}
	}
	return live, true
}

func pendingTabs(tabs []SavedTab) []SavedTab {
	var out []SavedTab
	for _, t := range tabs {
		if t.State == TabPending {
			out = append(out, t)
		}
	}
	return out
}

func hasSavedURL(tabs []SavedTab, rawURL string) bool {
	saved := make([]Tab, len(tabs))
	for i, t := range tabs {
		saved[i] = Tab{URL: t.URL}
	}
	_, ok := MatchTab(saved, rawURL)
	return ok
}

func isWebURL(u string) bool {
	lower := strings.ToLower(u)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}
