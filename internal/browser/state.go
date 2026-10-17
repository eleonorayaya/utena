package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
)

const liveStateTimeout = time.Second

type SessionsView struct {
	Connected  bool                    `json:"connected"`
	Outdated   bool                    `json:"outdated"`
	UpdateHint string                  `json:"update_hint,omitempty"`
	Warning    string                  `json:"warning,omitempty"`
	Sessions   map[uint]SessionBrowser `json:"sessions"`
}

type SessionBrowser struct {
	State    string       `json:"state"`
	TabCount int          `json:"tab_count"`
	Windows  []WindowView `json:"windows"`
}

type WindowView struct {
	ID      string    `json:"id"`
	State   string    `json:"state"`
	Focused bool      `json:"focused"`
	Tabs    []TabView `json:"tabs"`
}

type TabView struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	URL    string   `json:"url"`
	Active bool     `json:"active"`
	State  TabState `json:"state"`
}

type FocusRequest struct {
	SessionID uint   `json:"session_id"`
	WindowID  string `json:"window_id"`
	TabID     string `json:"tab_id"`
}

func (r *FocusRequest) Bind(*http.Request) error {
	switch {
	case r.SessionID == 0:
		return errors.New("session_id is required")
	case r.WindowID == "":
		return errors.New("window_id is required")
	case r.TabID == "":
		return errors.New("tab_id is required")
	}
	return nil
}

type FocusResult struct {
	WindowID  string `json:"window_id"`
	TabID     string `json:"tab_id"`
	Focused   bool   `json:"focused"`
	Activated bool   `json:"activated"`
}

func (s *Service) Outdated() bool {
	r, ok := s.driver.(StatusReporter)
	return ok && r.Outdated()
}

func (s *Service) updateHint() string {
	if r, ok := s.driver.(StatusReporter); ok && r.Outdated() {
		return r.UpdateHint()
	}
	return ""
}

func (s *Service) warning() string {
	if r, ok := s.driver.(WarningReporter); ok {
		return r.Warning()
	}
	return ""
}

func stateRank(state WindowState) int {
	switch state {
	case WindowOpen:
		return 3
	case WindowMissing:
		return 2
	case WindowSnapshotted:
		return 1
	}
	return 0
}

func (s *Service) BrowserSessions(ctx context.Context) SessionsView {
	view := SessionsView{Connected: s.Connected(), Outdated: s.Outdated(), UpdateHint: s.updateHint(), Warning: s.warning(), Sessions: map[uint]SessionBrowser{}}
	rows, err := s.store.ListAll()
	if err != nil {
		slog.WarnContext(ctx, "browser: list windows for state failed", "error", err)
		return view
	}
	live := s.liveWindows(ctx, view.Connected, rows)
	for _, r := range rows {
		sb := view.Sessions[r.SessionID]
		wv := windowView(r, live)
		sb.Windows = append(sb.Windows, wv)
		sb.TabCount += len(wv.Tabs)
		if stateRank(r.State) > stateRank(WindowState(sb.State)) {
			sb.State = string(r.State)
		}
		view.Sessions[r.SessionID] = sb
	}
	return view
}

func (s *Service) liveWindows(ctx context.Context, connected bool, rows []BrowserWindow) map[string]Window {
	live := map[string]Window{}
	if !connected || !hasOpenRow(rows) {
		return live
	}
	ctx, cancel := context.WithTimeout(ctx, liveStateTimeout)
	defer cancel()
	windows, err := s.driver.ListWindows(ctx)
	if err != nil {
		slog.DebugContext(ctx, "browser: live windows unavailable for state", "error", err)
		return live
	}
	for _, w := range windows {
		live[w.ID] = w
	}
	return live
}

func hasOpenRow(rows []BrowserWindow) bool {
	for _, r := range rows {
		if r.State == WindowOpen {
			return true
		}
	}
	return false
}

func windowView(r BrowserWindow, live map[string]Window) WindowView {
	wv := WindowView{ID: r.DriverWindowID, State: string(r.State), Tabs: []TabView{}}
	if w, ok := live[r.DriverWindowID]; ok && r.State == WindowOpen && r.DriverWindowID != "" {
		wv.Focused = w.Focused
		for _, t := range w.Tabs {
			wv.Tabs = append(wv.Tabs, TabView{ID: t.ID, Title: t.Title, URL: t.URL, Active: t.Active, State: TabOpen})
		}
		for _, t := range pendingTabs(r.Tabs) {
			wv.Tabs = append(wv.Tabs, TabView{Title: t.Title, URL: t.URL, State: TabPending})
		}
		return wv
	}
	for _, t := range r.Tabs {
		wv.Tabs = append(wv.Tabs, TabView{Title: t.Title, URL: t.URL, Active: t.Active, State: t.StateIn(r.State)})
	}
	return wv
}

func (s *Service) focusTarget(ctx context.Context, req FocusRequest) (Window, []Window, error) {
	row, err := s.store.GetByDriverWindowID(req.WindowID)
	if errors.Is(err, ErrWindowNotFound) || (err == nil && row.SessionID != req.SessionID) {
		return Window{}, nil, common.NewInvalidRequest(fmt.Sprintf("window %s is not owned by session %d", req.WindowID, req.SessionID))
	}
	if err != nil {
		return Window{}, nil, err
	}
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return Window{}, nil, driverErr(err)
	}
	target, ok := findWindow(live, req.WindowID)
	if !ok {
		return Window{}, nil, ErrWindowNotFound
	}
	if !hasTab(target, req.TabID) {
		return Window{}, nil, common.NewInvalidRequest(fmt.Sprintf("tab %s is not in window %s", req.TabID, req.WindowID))
	}
	return target, live, nil
}

func (s *Service) FocusTab(ctx context.Context, req FocusRequest) (FocusResult, error) {
	if !s.Connected() {
		return FocusResult{}, ErrUnavailable
	}
	if _, _, err := s.focusTarget(ctx, req); err != nil {
		return FocusResult{}, err
	}

	res := FocusResult{WindowID: req.WindowID, TabID: req.TabID}
	attached, err := s.sessions.IsAttached(ctx, req.SessionID)
	switch {
	case errors.Is(err, ErrSessionNotFound):
		return FocusResult{}, common.WrapNotFound(fmt.Sprintf("session %d", req.SessionID), err)
	case err != nil:
		return FocusResult{}, err
	case !attached:
		if err := s.activate(ctx, req.SessionID); err != nil {
			return FocusResult{}, err
		}
		res.Activated = true
	}
	s.claimAttached(req.SessionID)

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected.Load() {
		return FocusResult{}, ErrUnavailable
	}
	target, live, err := s.focusTarget(ctx, req)
	if err != nil {
		return FocusResult{}, err
	}

	wmTarget, wmFound := s.wmTargetFor(ctx, live, target.ID)
	if err := s.driver.ActivateTab(ctx, target.ID, req.TabID); err != nil {
		return FocusResult{}, driverErr(err)
	}
	res.Focused = s.focusLocked(ctx, target.ID, wmTarget, wmFound)
	return res, nil
}

func hasTab(w Window, tabID string) bool {
	for _, t := range w.Tabs {
		if t.ID == tabID {
			return true
		}
	}
	return false
}
