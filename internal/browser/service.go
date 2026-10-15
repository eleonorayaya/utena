package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("browser unavailable")

type Sessions interface {
	IsAttached(ctx context.Context, sessionID uint) (bool, error)
	Activate(ctx context.Context, sessionID uint) error
}

type OpenRequest struct {
	SessionID uint   `json:"session_id"`
	URL       string `json:"url"`
	Activate  bool   `json:"activate"`
}

func (r *OpenRequest) Bind(*http.Request) error {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("url must be absolute: %q", r.URL)
	}
	return nil
}

type OpenResult struct {
	WindowID string `json:"window_id"`
	TabID    string `json:"tab_id"`
	Created  bool   `json:"created"`
	Focused  bool   `json:"focused"`
}

type Service struct {
	driver   Driver
	store    *Store
	sessions Sessions
	now      func() time.Time

	mu                  sync.Mutex
	connected           bool
	lastFocusedWindowID string
}

func NewService(driver Driver, store *Store, sessions Sessions) *Service {
	return &Service{driver: driver, store: store, sessions: sessions, now: time.Now}
}

func (s *Service) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *Service) Open(ctx context.Context, req OpenRequest) (OpenResult, error) {
	if !s.Connected() {
		return OpenResult{}, ErrUnavailable
	}

	focus := true
	if req.SessionID != 0 {
		attached, err := s.sessions.IsAttached(ctx, req.SessionID)
		if err != nil {
			return OpenResult{}, err
		}
		if req.Activate && !attached {
			if err := s.sessions.Activate(ctx, req.SessionID); err != nil {
				return OpenResult{}, err
			}
			attached = true
		}
		focus = attached
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected {
		return OpenResult{}, ErrUnavailable
	}

	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return OpenResult{}, err
	}
	target, found, err := s.targetWindow(req.SessionID, live)
	if err != nil {
		return OpenResult{}, err
	}

	var res OpenResult
	switch {
	case !found:
		w, err := s.driver.CreateWindow(ctx, []string{req.URL}, focus)
		if err != nil {
			return OpenResult{}, err
		}
		if req.SessionID != 0 {
			row := &BrowserWindow{SessionID: req.SessionID, State: WindowOpen, DriverWindowID: w.ID, LastFocusedAt: s.now()}
			if err := s.store.Add(row); err != nil {
				return OpenResult{}, err
			}
		}
		res = OpenResult{WindowID: w.ID, Created: true}
		if len(w.Tabs) > 0 {
			res.TabID = w.Tabs[0].ID
		}
	default:
		res.WindowID = target.ID
		if tab, ok := MatchTab(target.Tabs, req.URL); ok {
			if err := s.driver.ActivateTab(ctx, target.ID, tab.ID); err != nil {
				return OpenResult{}, err
			}
			res.TabID = tab.ID
		} else {
			tab, err := s.driver.OpenTab(ctx, target.ID, req.URL)
			if err != nil {
				return OpenResult{}, err
			}
			res.TabID = tab.ID
		}
	}

	if focus {
		if err := s.driver.FocusWindow(ctx, res.WindowID); err != nil {
			slog.WarnContext(ctx, "browser: focus window failed", "window", res.WindowID, "error", err)
		} else {
			res.Focused = true
		}
	}
	return res, nil
}

func (s *Service) targetWindow(sessionID uint, live []Window) (Window, bool, error) {
	byID := make(map[string]Window, len(live))
	for _, w := range live {
		byID[w.ID] = w
	}

	if sessionID == 0 {
		owned, err := s.store.OwnedDriverWindowIDs()
		if err != nil {
			return Window{}, false, err
		}
		if w, ok := byID[s.lastFocusedWindowID]; ok && !owned[w.ID] {
			return w, true, nil
		}
		for _, w := range live {
			if !owned[w.ID] {
				return w, true, nil
			}
		}
		return Window{}, false, nil
	}

	rows, err := s.store.ListOpenBySession(sessionID)
	if err != nil {
		return Window{}, false, err
	}
	for _, r := range rows {
		if w, ok := byID[r.DriverWindowID]; ok {
			return w, true, nil
		}
	}
	return Window{}, false, nil
}

func (s *Service) handleEvent(ctx context.Context, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Type {
	case EventConnected:
		s.connected = true
	case EventDisconnected:
		s.connected = false
	case EventFocusChanged:
		if ev.WindowID == "" {
			return
		}
		s.lastFocusedWindowID = ev.WindowID
		if err := s.store.TouchFocused(ev.WindowID, s.now()); err != nil {
			slog.WarnContext(ctx, "browser: record focus failed", "window", ev.WindowID, "error", err)
		}
	}
}
