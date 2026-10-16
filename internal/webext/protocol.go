package webext

import (
	"encoding/json"
	"strconv"

	"github.com/eleonorayaya/utena/internal/browser"
)

const ProtocolVersion = 2

type message struct {
	Type     string          `json:"type"`
	ID       int64           `json:"id,omitempty"`
	Method   string          `json:"method,omitempty"`
	Params   any             `json:"params,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	Event    string          `json:"event,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Protocol int             `json:"protocol,omitempty"`
	Browser  string          `json:"browser,omitempty"`
	Run      string          `json:"run,omitempty"`
}

type wireTab struct {
	ID     int64  `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
}

type wireWindow struct {
	ID      int64     `json:"id"`
	Focused bool      `json:"focused"`
	Tabs    []wireTab `json:"tabs"`
}

type windowEventData struct {
	WindowID *int64 `json:"windowId"`
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (t wireTab) toTab() browser.Tab {
	return browser.Tab{ID: formatID(t.ID), URL: t.URL, Title: t.Title, Active: t.Active, Pinned: t.Pinned}
}

func (w wireWindow) toWindow() browser.Window {
	out := browser.Window{ID: formatID(w.ID), Focused: w.Focused, Tabs: make([]browser.Tab, len(w.Tabs))}
	for i, t := range w.Tabs {
		out.Tabs[i] = t.toTab()
	}
	return out
}

func translateEvent(m message) (browser.Event, bool) {
	var d windowEventData
	_ = json.Unmarshal(m.Data, &d)
	id := ""
	if d.WindowID != nil && *d.WindowID >= 0 {
		id = formatID(*d.WindowID)
	}
	switch m.Event {
	case "window.created":
		return browser.Event{Type: browser.EventWindowCreated, WindowID: id}, id != ""
	case "window.removed":
		return browser.Event{Type: browser.EventWindowRemoved, WindowID: id}, id != ""
	case "window.focused":
		return browser.Event{Type: browser.EventFocusChanged, WindowID: id}, true
	case "tabs.changed":
		return browser.Event{Type: browser.EventTabsChanged, WindowID: id}, id != ""
	}
	return browser.Event{}, false
}
