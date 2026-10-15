package browser

import (
	"context"
	"errors"
	"net/http"
)

var ErrNotConnected = errors.New("browser not connected")

type Driver interface {
	ListWindows(ctx context.Context) ([]Window, error)
	CreateWindow(ctx context.Context, urls []string, focused bool) (Window, error)
	OpenTab(ctx context.Context, windowID, url string) (Tab, error)
	ActivateTab(ctx context.Context, windowID, tabID string) error
	FocusWindow(ctx context.Context, windowID string) error
	CloseWindow(ctx context.Context, windowID string) error
	Subscribe(ctx context.Context, onEvent func(Event)) error
	AppBundleID() string
	WindowTitle(w Window) string
}

type HTTPDriver interface {
	Name() string
	Handler() http.Handler
}

type Window struct {
	ID      string `json:"id"`
	Focused bool   `json:"focused"`
	Tabs    []Tab  `json:"tabs"`
}

func (w Window) ActiveTab() (Tab, bool) {
	for _, t := range w.Tabs {
		if t.Active {
			return t, true
		}
	}
	return Tab{}, false
}

type Tab struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
}

type EventType string

const (
	EventConnected     EventType = "connected"
	EventDisconnected  EventType = "disconnected"
	EventWindowCreated EventType = "window_created"
	EventWindowRemoved EventType = "window_removed"
	EventTabsChanged   EventType = "tabs_changed"
	EventFocusChanged  EventType = "focus_changed"
)

type Event struct {
	Type     EventType
	WindowID string
	Run      string
}
