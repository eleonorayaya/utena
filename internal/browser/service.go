package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrUnavailable     = errors.New("browser unavailable")
	ErrSessionNotFound = errors.New("browser: session not found")
)

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
	WindowID  string `json:"window_id"`
	TabID     string `json:"tab_id"`
	Created   bool   `json:"created"`
	Focused   bool   `json:"focused"`
	Activated bool   `json:"activated"`
}

type Service struct {
	driver   Driver
	store    *Store
	sessions Sessions
	now      func() time.Time

	mu                  sync.Mutex
	connected           atomic.Bool
	lastFocusedWindowID string
	prevFocusedWindowID string
	unowned             map[string]struct{}
	run                 string
	runLoaded           bool
}

func NewService(driver Driver, store *Store, sessions Sessions) *Service {
	return &Service{driver: driver, store: store, sessions: sessions, now: time.Now, unowned: map[string]struct{}{}}
}

func driverErr(err error) error {
	if errors.Is(err, ErrNotConnected) {
		return ErrUnavailable
	}
	return err
}

func (s *Service) Connected() bool {
	return s.connected.Load()
}

func (s *Service) Open(ctx context.Context, req OpenRequest) (OpenResult, error) {
	if !s.Connected() {
		return OpenResult{}, ErrUnavailable
	}

	focus := true
	activated := false
	if req.SessionID != 0 {
		attached, err := s.sessions.IsAttached(ctx, req.SessionID)
		switch {
		case errors.Is(err, ErrSessionNotFound):
			req.SessionID = 0
		case err != nil:
			return OpenResult{}, err
		case req.Activate && !attached:
			if err := s.sessions.Activate(ctx, req.SessionID); err != nil {
				return OpenResult{}, err
			}
			activated = true
		default:
			focus = attached
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected.Load() {
		return OpenResult{}, ErrUnavailable
	}

	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return OpenResult{}, driverErr(err)
	}
	target, found, err := s.targetWindow(req.SessionID, live)
	if err != nil {
		return OpenResult{}, err
	}

	res := OpenResult{Activated: activated}
	switch {
	case !found:
		w, err := s.driver.CreateWindow(ctx, []string{req.URL}, focus)
		if err != nil {
			return OpenResult{}, driverErr(err)
		}
		if req.SessionID == 0 {
			s.unowned[w.ID] = struct{}{}
		} else {
			row := &BrowserWindow{SessionID: req.SessionID, State: WindowOpen, DriverWindowID: w.ID, LastFocusedAt: s.now()}
			if err := s.store.Add(row); err != nil {
				return OpenResult{}, err
			}
		}
		res.WindowID, res.Created = w.ID, true
		if len(w.Tabs) > 0 {
			res.TabID = w.Tabs[0].ID
		}
	default:
		res.WindowID = target.ID
		if tab, ok := MatchTab(target.Tabs, req.URL); ok {
			if err := s.driver.ActivateTab(ctx, target.ID, tab.ID); err != nil {
				return OpenResult{}, driverErr(err)
			}
			res.TabID = tab.ID
		} else {
			tab, err := s.driver.OpenTab(ctx, target.ID, req.URL)
			if err != nil {
				return OpenResult{}, driverErr(err)
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

func (s *Service) OnAppStart(ctx context.Context) error {
	go s.followEvents(ctx)
	return nil
}

func (s *Service) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *Service) followEvents(ctx context.Context) {
	lastErr := ""
	for ctx.Err() == nil {
		err := s.driver.Subscribe(ctx, func(ev Event) {
			lastErr = ""
			s.handleEvent(ctx, ev)
		})
		s.handleEvent(ctx, Event{Type: EventDisconnected})
		if ctx.Err() != nil {
			return
		}
		if err != nil && err.Error() != lastErr {
			lastErr = err.Error()
			slog.Warn("browser event stream unavailable; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Service) handleEvent(ctx context.Context, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	switch ev.Type {
	case EventConnected:
		s.connected.Store(true)
		err = errors.Join(s.trackRun(ev.Run), s.resync(ctx))
	case EventDisconnected:
		s.connected.Store(false)
	case EventWindowCreated:
		err = s.adopt(ev.WindowID)
	case EventWindowRemoved:
		err = s.store.DeleteByDriverWindowID(ev.WindowID)
	case EventFocusChanged:
		if ev.WindowID == "" {
			return
		}
		if ev.WindowID != s.lastFocusedWindowID {
			s.prevFocusedWindowID = s.lastFocusedWindowID
			s.lastFocusedWindowID = ev.WindowID
		}
		err = s.store.TouchFocused(ev.WindowID, s.now())
	}
	if err != nil {
		slog.WarnContext(ctx, "browser event handler failed", "type", ev.Type, "window", ev.WindowID, "error", err)
	}
}

func (s *Service) trackRun(run string) error {
	if run == "" {
		return nil
	}
	if !s.runLoaded {
		stored, err := s.store.RunID()
		if err != nil {
			return err
		}
		s.run, s.runLoaded = stored, true
	}
	if run == s.run {
		return nil
	}
	if err := s.store.MarkMissingExcept(nil); err != nil {
		return err
	}
	if err := s.store.SetRunID(run); err != nil {
		return err
	}
	s.run = run
	return nil
}

func (s *Service) resync(ctx context.Context) error {
	windows, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	s.unowned = map[string]struct{}{}
	s.lastFocusedWindowID = ""
	s.prevFocusedWindowID = ""
	live := make(map[string]bool, len(windows))
	for _, w := range windows {
		live[w.ID] = true
		if w.Focused {
			s.lastFocusedWindowID = w.ID
		}
	}
	return s.store.MarkMissingExcept(live)
}

func (s *Service) adopt(windowID string) error {
	if _, ok := s.unowned[windowID]; ok {
		delete(s.unowned, windowID)
		return nil
	}
	if _, err := s.store.GetByDriverWindowID(windowID); err == nil {
		return nil
	} else if !errors.Is(err, ErrWindowNotFound) {
		return err
	}
	parentID := s.lastFocusedWindowID
	if parentID == windowID {
		parentID = s.prevFocusedWindowID
	}
	parent, err := s.store.GetByDriverWindowID(parentID)
	if errors.Is(err, ErrWindowNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.Add(&BrowserWindow{SessionID: parent.SessionID, State: WindowOpen, DriverWindowID: windowID, LastFocusedAt: s.now()})
}
