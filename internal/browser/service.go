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

	"github.com/eleonorayaya/utena/internal/wm"
)

var (
	ErrUnavailable     = errors.New("browser unavailable")
	ErrSessionNotFound = errors.New("browser: session not found")
)

type Sessions interface {
	IsAttached(ctx context.Context, sessionID uint) (bool, error)
	Activate(ctx context.Context, sessionID uint) error
	Attached(ctx context.Context) (uint, error)
}

const (
	createdLinkPoll = 100 * time.Millisecond
	createdLinkWait = 1500 * time.Millisecond
	snapshotDelay   = 2 * time.Second
	removeGrace     = 2 * time.Second
)

type pendingTimer struct {
	stop func() bool
}

type lifecycleOp struct {
	sessionID uint
	deleted   bool
}

type pendingCloseOp struct {
	rowID   uint
	deleted bool
}

type Layout struct {
	BrowserWorkspace string
	ParkWorkspace    string
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
	wm       wm.Driver
	wmInert  bool
	layout   Layout
	now      func() time.Time
	attached atomic.Uint64
	swapCh   chan struct{}

	lifecycleMu sync.Mutex
	lifecycle   []lifecycleOp
	lifecycleCh chan struct{}

	afterFunc     func(time.Duration, func()) func() bool
	snapshotDelay time.Duration
	removeGrace   time.Duration
	restoreWindow time.Duration

	mu                  sync.Mutex
	connected           atomic.Bool
	lastFocusedWindowID string
	prevFocusedWindowID string
	unowned             map[string]struct{}
	run                 string
	runLoaded           bool
	links               linker
	placedFor           uint
	placeStale          bool
	snapshots           map[string]*pendingTimer
	removals            map[string]*pendingTimer
	restoreUntil        time.Time
	deferredAdopt       map[string]uint
	daemonUnowned       map[string]struct{}
	closing             map[string]struct{}
	reattachRejected    map[string]struct{}
	pendingClose        map[string]pendingCloseOp
	restoreTimer        func() bool
}

func NewService(driver Driver, store *Store, sessions Sessions, wmDriver wm.Driver, layout Layout) *Service {
	return &Service{
		driver:   driver,
		store:    store,
		sessions: sessions,
		wm:       wmDriver,
		wmInert:  isInert(wmDriver),
		layout:   layout,
		now:      time.Now,
		swapCh:   make(chan struct{}, 1),
		unowned:  map[string]struct{}{},

		lifecycleCh: make(chan struct{}, 1),
		closing:     map[string]struct{}{},

		reattachRejected: map[string]struct{}{},
		pendingClose:     map[string]pendingCloseOp{},

		deferredAdopt: map[string]uint{},
		daemonUnowned: map[string]struct{}{},

		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		snapshotDelay: snapshotDelay,
		removeGrace:   removeGrace,
		snapshots:     map[string]*pendingTimer{},
		removals:      map[string]*pendingTimer{},
	}
}

func isInert(d wm.Driver) bool {
	switch d.(type) {
	case wm.None, *wm.None:
		return true
	}
	return false
}

func (s *Service) SetAttached(sessionID uint) {
	s.attached.Store(uint64(sessionID))
	s.signal()
}

func (s *Service) signal() {
	select {
	case s.swapCh <- struct{}{}:
	default:
	}
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
			s.SetAttached(req.SessionID)
		default:
			focus = attached
		}
	}
	if focus && req.SessionID != 0 && uint(s.attached.Load()) != req.SessionID {
		s.SetAttached(req.SessionID)
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
	if !found && req.SessionID != 0 {
		restored, err := s.restoreLocked(ctx, req.SessionID)
		if err != nil {
			if len(restored) == 0 && errors.Is(err, ErrUnavailable) {
				return OpenResult{}, err
			}
			slog.WarnContext(ctx, "browser: restore incomplete", "session", req.SessionID, "error", err)
		}
		if len(restored) > 0 {
			if live, err = s.driver.ListWindows(ctx); err != nil {
				return OpenResult{}, driverErr(err)
			}
			target, found = restored[0], true
			if w, ok := findWindow(live, target.ID); ok {
				target = w
			}
		}
	}

	res := OpenResult{Activated: activated}
	var wmTarget wm.Window
	wmFound := false
	switch {
	case !found:
		before, track := s.wmSnapshot(ctx)
		w, err := s.driver.CreateWindow(ctx, []string{req.URL}, focus)
		if err != nil {
			return OpenResult{}, driverErr(err)
		}
		if req.SessionID == 0 {
			s.unowned[w.ID] = struct{}{}
			s.daemonUnowned[w.ID] = struct{}{}
		} else {
			now := s.now()
			row := &BrowserWindow{SessionID: req.SessionID, State: WindowOpen, DriverWindowID: w.ID, LastFocusedAt: now, Tabs: savedTabs(w), SnapshotAt: now}
			if err := s.store.Add(row); err != nil {
				return OpenResult{}, err
			}
			s.signal()
		}
		if track {
			s.linkCreated(ctx, w.ID, before, live, time.Now().Add(createdLinkWait))
		}
		if focus {
			wmTarget, wmFound = s.wmFocusTarget(ctx, func([]wm.Window) (string, bool) {
				wmID, ok := s.links.links[w.ID]
				return wmID, ok
			})
		}
		res.WindowID, res.Created = w.ID, true
		if len(w.Tabs) > 0 {
			res.TabID = w.Tabs[0].ID
		}
	default:
		if focus {
			s.ensurePlacedLocked(ctx)
			wmTarget, wmFound = s.wmFocusTarget(ctx, func(wmWindows []wm.Window) (string, bool) {
				wmID, ok := s.links.resolve(live, s.driver.WindowTitle, wmWindows, []string{target.ID})[target.ID]
				return wmID, ok
			})
		}
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
		switch {
		case s.wmInert:
		case !wmFound:
			slog.WarnContext(ctx, "browser: no wm window to focus", "window", res.WindowID)
		default:
			if s.wmFocus(ctx, res.WindowID, wmTarget) {
				res.Focused = true
			}
		}
	}
	return res, nil
}

func (s *Service) wmFocus(ctx context.Context, windowID string, target wm.Window) bool {
	if target.Workspace == s.layout.ParkWorkspace {
		if err := s.wm.MoveToWorkspace(ctx, target.ID, s.layout.BrowserWorkspace); err != nil {
			slog.WarnContext(ctx, "browser: unpark before focus failed", "window", windowID, "wm_window", target.ID, "error", err)
			return false
		}
	}
	if err := s.wm.FocusWindow(ctx, target.ID); err != nil {
		slog.WarnContext(ctx, "browser: wm focus window failed", "window", windowID, "wm_window", target.ID, "error", err)
		return false
	}
	return true
}

func (s *Service) wmFocusTarget(ctx context.Context, link func([]wm.Window) (string, bool)) (wm.Window, bool) {
	if s.wmInert {
		return wm.Window{}, false
	}
	wmWindows, err := s.wm.ListWindows(ctx, s.driver.AppBundleID())
	if err != nil {
		slog.WarnContext(ctx, "browser: list wm windows failed", "error", err)
		return wm.Window{}, false
	}
	wmID, ok := link(wmWindows)
	if !ok {
		return wm.Window{}, false
	}
	for _, w := range wmWindows {
		if w.ID == wmID {
			return w, true
		}
	}
	return wm.Window{}, false
}

func (s *Service) wmSnapshot(ctx context.Context) ([]wm.Window, bool) {
	if s.wmInert {
		return nil, false
	}
	before, err := s.wm.ListWindows(ctx, s.driver.AppBundleID())
	if err != nil {
		slog.WarnContext(ctx, "browser: list wm windows failed", "error", err)
		return nil, false
	}
	return before, true
}

func (s *Service) linkCreated(ctx context.Context, windowID string, before []wm.Window, live []Window, deadline time.Time) {
	for {
		after, err := s.wm.ListWindows(ctx, s.driver.AppBundleID())
		if err != nil {
			slog.WarnContext(ctx, "browser: list wm windows failed; created window not linked", "window", windowID, "error", err)
			return
		}
		if s.links.linkCreated(windowID, before, after) {
			if !s.onlyNewBrowserWindow(ctx, windowID, live) {
				delete(s.links.links, windowID)
				slog.DebugContext(ctx, "browser: created window link dropped; another browser window appeared", "window", windowID)
			}
			return
		}
		if !time.Now().Before(deadline) {
			slog.DebugContext(ctx, "browser: created window not linked", "window", windowID)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(createdLinkPoll):
		}
	}
}

func (s *Service) onlyNewBrowserWindow(ctx context.Context, windowID string, live []Window) bool {
	now, err := s.driver.ListWindows(ctx)
	if err != nil {
		return false
	}
	existing := make(map[string]bool, len(live))
	for _, w := range live {
		existing[w.ID] = true
	}
	for _, w := range now {
		if !existing[w.ID] && w.ID != windowID {
			return false
		}
	}
	return true
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
	go s.placeLoop(ctx)
	go s.lifecycleLoop(ctx)
	return nil
}

func (s *Service) QueueArchive(sessionID uint) {
	s.enqueue(lifecycleOp{sessionID: sessionID})
}

func (s *Service) QueueDelete(sessionID uint) {
	s.enqueue(lifecycleOp{sessionID: sessionID, deleted: true})
}

func (s *Service) enqueue(op lifecycleOp) {
	s.lifecycleMu.Lock()
	s.lifecycle = append(s.lifecycle, op)
	s.lifecycleMu.Unlock()
	select {
	case s.lifecycleCh <- struct{}{}:
	default:
	}
}

func (s *Service) lifecycleLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.lifecycleCh:
			s.lifecycleMu.Lock()
			ops := s.lifecycle
			s.lifecycle = nil
			s.lifecycleMu.Unlock()
			for _, op := range ops {
				if err := s.runLifecycle(ctx, op); err != nil {
					slog.WarnContext(ctx, "browser: session lifecycle failed", "session", op.sessionID, "deleted", op.deleted, "error", err)
				}
			}
		}
	}
}

func (s *Service) runLifecycle(ctx context.Context, op lifecycleOp) error {
	if op.deleted {
		return s.Delete(ctx, op.sessionID)
	}
	if attached, err := s.sessions.IsAttached(ctx, op.sessionID); err == nil && attached {
		slog.DebugContext(ctx, "browser: archive skipped; session attached again", "session", op.sessionID)
		return nil
	}
	return s.Archive(ctx, op.sessionID)
}

func (s *Service) dropDeferredInto(sessionID uint) {
	for id, parent := range s.deferredAdopt {
		if parent == sessionID {
			delete(s.deferredAdopt, id)
		}
	}
}

func (s *Service) Archive(ctx context.Context, sessionID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, live, err := s.releaseSessionLocked(ctx, sessionID)
	if err != nil {
		return err
	}
	connected := live != nil

	now := s.now()
	var errs []error
	for _, r := range rows {
		if _, ok := s.removals[r.DriverWindowID]; ok {
			continue
		}
		w, ok := live[r.DriverWindowID]
		if !ok {
			if !connected && r.DriverWindowID != "" {
				s.pendingClose[r.DriverWindowID] = pendingCloseOp{rowID: r.ID}
			}
			errs = append(errs, s.store.MarkSnapshotted(r.ID, r.Tabs, r.SnapshotAt))
			continue
		}
		tabs, at := savedTabs(w), now
		if len(tabs) == 0 {
			tabs, at = r.Tabs, r.SnapshotAt
		}
		if err := s.store.MarkSnapshotted(r.ID, tabs, at); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := s.closeWindow(ctx, w.ID); err != nil {
			errs = append(errs, err, s.store.Reopen(r.ID, w.ID, r.LastFocusedAt))
		}
	}

	restorable, err := s.store.ListRestorable(sessionID)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, r := range restorable {
		if r.State == WindowMissing {
			errs = append(errs, s.store.MarkSnapshotted(r.ID, r.Tabs, r.SnapshotAt))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) Delete(ctx context.Context, sessionID uint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, live, err := s.releaseSessionLocked(ctx, sessionID)
	if err != nil {
		return err
	}
	var errs []error
	if live != nil {
		for _, r := range rows {
			if _, ok := live[r.DriverWindowID]; ok {
				errs = append(errs, s.closeWindow(ctx, r.DriverWindowID))
			}
		}
	} else {
		for _, r := range rows {
			if r.DriverWindowID != "" {
				s.pendingClose[r.DriverWindowID] = pendingCloseOp{rowID: r.ID, deleted: true}
			}
		}
	}
	return errors.Join(append(errs, s.store.DeleteBySession(sessionID))...)
}

func (s *Service) releaseSessionLocked(ctx context.Context, sessionID uint) ([]BrowserWindow, map[string]Window, error) {
	s.attached.CompareAndSwap(uint64(sessionID), 0)
	s.dropDeferredInto(sessionID)
	rows, err := s.store.ListOpenBySession(sessionID)
	if err != nil || !s.connected.Load() {
		return rows, nil, err
	}
	windows, err := s.driver.ListWindows(ctx)
	if err != nil {
		return nil, nil, driverErr(err)
	}
	live := make(map[string]Window, len(windows))
	for _, w := range windows {
		live[w.ID] = w
	}
	return rows, live, nil
}

func (s *Service) closeWindow(ctx context.Context, windowID string) error {
	stopTimer(s.snapshots, windowID)
	s.closing[windowID] = struct{}{}
	if err := s.driver.CloseWindow(ctx, windowID); err != nil {
		delete(s.closing, windowID)
		return driverErr(err)
	}
	return nil
}

func (s *Service) restoreLocked(ctx context.Context, sessionID uint) ([]Window, error) {
	if !s.connected.Load() {
		return nil, nil
	}
	rows, err := s.store.ListRestorable(sessionID)
	if err != nil {
		return nil, err
	}
	restoring := s.restoring()
	deadline := time.Now().Add(createdLinkWait)
	var restored []Window
	var errs []error
	for _, r := range rows {
		if restoring && r.State == WindowMissing {
			continue
		}
		w, err := s.restoreRow(ctx, r, deadline)
		if err != nil {
			errs = append(errs, driverErr(err))
			continue
		}
		restored = append(restored, w)
		errs = append(errs, s.activateSaved(ctx, r, w), s.snapshot(ctx, w.ID))
	}
	if len(restored) > 0 {
		s.placeStale = true
		s.signal()
	}
	return restored, errors.Join(errs...)
}

func (s *Service) restoreRow(ctx context.Context, r BrowserWindow, deadline time.Time) (Window, error) {
	urls := savedURLs(r.Tabs)
	if len(urls) == 0 {
		urls = []string{"chrome://newtab/"}
	}
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return Window{}, err
	}
	before, track := s.wmSnapshot(ctx)
	w, err := s.driver.CreateWindow(ctx, urls, false)
	if err != nil {
		return Window{}, err
	}
	if err := s.store.Reopen(r.ID, w.ID, r.LastFocusedAt); err != nil {
		if closeErr := s.closeWindow(ctx, w.ID); closeErr != nil {
			slog.WarnContext(ctx, "browser: close unrecorded restored window failed", "window", w.ID, "error", closeErr)
		}
		return Window{}, err
	}
	if track {
		s.linkCreated(ctx, w.ID, before, live, deadline)
	}
	return w, nil
}

func (s *Service) activateSaved(ctx context.Context, r BrowserWindow, w Window) error {
	for i, t := range r.Tabs {
		if t.Active && i < len(w.Tabs) {
			return driverErr(s.driver.ActivateTab(ctx, w.ID, w.Tabs[i].ID))
		}
	}
	return nil
}

func (s *Service) restoreAttached(ctx context.Context, attached uint, live []Window) (bool, error) {
	rows, err := s.store.ListOpenBySession(attached)
	if err != nil {
		return false, err
	}
	for _, r := range rows {
		if _, ok := findWindow(live, r.DriverWindowID); ok {
			return false, nil
		}
	}
	restored, err := s.restoreLocked(ctx, attached)
	return len(restored) > 0, err
}

func (s *Service) placeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.swapCh:
			if err := s.place(ctx); err != nil {
				slog.WarnContext(ctx, "browser: placement failed", "error", err)
			}
		}
	}
}

func (s *Service) place(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.placeLocked(ctx)
}

func (s *Service) placeLocked(ctx context.Context) error {
	if !s.connected.Load() {
		return nil
	}
	attached := uint(s.attached.Load())
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	if attached != 0 {
		restored, err := s.restoreAttached(ctx, attached, live)
		if err != nil {
			slog.WarnContext(ctx, "browser: restore attached session failed", "session", attached, "error", err)
		}
		if restored {
			if live, err = s.driver.ListWindows(ctx); err != nil {
				return err
			}
		}
	}
	if s.wmInert {
		s.placedFor, s.placeStale = attached, false
		return nil
	}
	wmWindows, err := s.wm.ListWindows(ctx, s.driver.AppBundleID())
	if err != nil {
		return err
	}
	rows, err := s.store.ListOpen()
	if err != nil {
		return err
	}

	want := make([]string, 0, len(rows))
	for _, r := range rows {
		want = append(want, r.DriverWindowID)
	}
	links := s.links.resolve(live, s.driver.WindowTitle, wmWindows, want)
	workspaces := make(map[string]string, len(wmWindows))
	for _, w := range wmWindows {
		workspaces[w.ID] = w.Workspace
	}

	for _, r := range rows {
		wmID, ok := links[r.DriverWindowID]
		if !ok {
			slog.DebugContext(ctx, "browser: window not linked to wm window", "window", r.DriverWindowID, "session", r.SessionID)
			continue
		}
		target := s.layout.ParkWorkspace
		if attached != 0 && r.SessionID == attached {
			target = s.layout.BrowserWorkspace
		}
		if workspaces[wmID] == target {
			continue
		}
		if err := s.wm.MoveToWorkspace(ctx, wmID, target); err != nil {
			slog.WarnContext(ctx, "browser: move window failed", "window", r.DriverWindowID, "wm_window", wmID, "workspace", target, "error", err)
		}
	}
	s.placedFor, s.placeStale = attached, false
	return nil
}

func (s *Service) ensurePlacedLocked(ctx context.Context) {
	if !s.placeStale && s.placedFor == uint(s.attached.Load()) {
		return
	}
	if err := s.placeLocked(ctx); err != nil {
		slog.WarnContext(ctx, "browser: placement before focus failed", "error", err)
	}
}

func (s *Service) OnAppEnd(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.connected.Store(false)
	stopTimers(s.snapshots)
	stopTimers(s.removals)
	if s.restoreTimer != nil {
		s.restoreTimer()
	}
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
		flushErr := s.flushRemovals()
		restarted, runErr := s.trackRun(ev.Run)
		if restarted && s.restoreWindow > 0 {
			s.restoreUntil = s.now().Add(s.restoreWindow)
			if s.restoreTimer != nil {
				s.restoreTimer()
			}
			s.restoreTimer = s.afterFunc(s.restoreWindow, s.signal)
		}
		err = errors.Join(flushErr, runErr, s.resync(ctx, restarted))
		s.signal()
	case EventDisconnected:
		s.connected.Store(false)
		err = s.flushRemovals()
		stopTimers(s.snapshots)
		clear(s.closing)
	case EventWindowCreated:
		err = s.adopt(ctx, ev.WindowID)
	case EventWindowRemoved:
		err = s.windowRemoved(ev.WindowID)
	case EventTabsChanged:
		err = s.tabsChanged(ctx, ev.WindowID)
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

func (s *Service) tabsChanged(ctx context.Context, windowID string) error {
	if parent, ok := s.deferredAdopt[windowID]; ok {
		live, err := s.driver.ListWindows(ctx)
		if err != nil {
			return err
		}
		return s.adoptDeferred(ctx, windowID, parent, live)
	}
	if _, err := s.store.GetByDriverWindowID(windowID); errors.Is(err, ErrWindowNotFound) {
		if _, ok := s.unowned[windowID]; ok {
			return nil
		}
		if !s.restoring() {
			return nil
		}
		live, err := s.driver.ListWindows(ctx)
		if err != nil {
			return err
		}
		_, err = s.reattachLate(ctx, windowID, live)
		return err
	} else if err != nil {
		return err
	}
	s.scheduleSnapshot(windowID)
	return nil
}

func (s *Service) windowRemoved(windowID string) error {
	delete(s.links.links, windowID)
	delete(s.deferredAdopt, windowID)
	delete(s.daemonUnowned, windowID)
	delete(s.reattachRejected, windowID)
	stopTimer(s.snapshots, windowID)
	if _, ok := s.closing[windowID]; ok {
		delete(s.closing, windowID)
		return nil
	}
	if _, ok := s.removals[windowID]; ok {
		return nil
	}
	if _, err := s.store.GetByDriverWindowID(windowID); errors.Is(err, ErrWindowNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	s.schedule(s.removals, windowID, s.removeGrace, func() {
		if !s.connected.Load() {
			return
		}
		if err := s.store.DeleteByDriverWindowID(windowID); err != nil {
			slog.WarnContext(context.Background(), "browser: forget closed window failed", "window", windowID, "error", err)
		}
	})
	return nil
}

func (s *Service) flushRemovals() error {
	var errs []error
	for id := range s.removals {
		stopTimer(s.removals, id)
		errs = append(errs, s.store.MarkMissingByDriverWindowID(id))
	}
	return errors.Join(errs...)
}

func stopTimer(timers map[string]*pendingTimer, key string) {
	if t, ok := timers[key]; ok {
		t.stop()
		delete(timers, key)
	}
}

func stopTimers(timers map[string]*pendingTimer) {
	for key := range timers {
		stopTimer(timers, key)
	}
}

func (s *Service) schedule(timers map[string]*pendingTimer, key string, d time.Duration, f func()) {
	t := &pendingTimer{}
	timers[key] = t
	t.stop = s.afterFunc(d, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if timers[key] != t {
			return
		}
		delete(timers, key)
		f()
	})
}

func (s *Service) scheduleSnapshot(windowID string) {
	if _, ok := s.snapshots[windowID]; ok {
		return
	}
	s.schedule(s.snapshots, windowID, s.snapshotDelay, func() {
		if !s.connected.Load() {
			return
		}
		ctx := context.Background()
		if err := s.snapshot(ctx, windowID); err != nil {
			slog.WarnContext(ctx, "browser: snapshot failed", "window", windowID, "error", err)
		}
		if _, ok := s.links.links[windowID]; !ok {
			s.signal()
		}
	})
}

func (s *Service) snapshot(ctx context.Context, windowID string) error {
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	if w, ok := findWindow(live, windowID); ok {
		return s.saveTabs(w, s.now())
	}
	return nil
}

func (s *Service) saveTabs(w Window, at time.Time) error {
	tabs := savedTabs(w)
	if len(tabs) == 0 {
		return nil
	}
	return s.store.SaveTabs(w.ID, tabs, at)
}

func (s *Service) snapshotOwned(windows []Window) error {
	owned, err := s.store.OwnedDriverWindowIDs()
	if err != nil {
		return err
	}
	now := s.now()
	var errs []error
	for _, w := range windows {
		if owned[w.ID] {
			errs = append(errs, s.saveTabs(w, now))
		}
	}
	return errors.Join(errs...)
}

func (s *Service) trackRun(run string) (bool, error) {
	if run == "" {
		return false, nil
	}
	if !s.runLoaded {
		stored, err := s.store.RunID()
		if err != nil {
			return false, err
		}
		s.run, s.runLoaded = stored, true
	}
	if run == s.run {
		return false, nil
	}
	if err := s.store.MarkMissingExcept(nil); err != nil {
		return false, err
	}
	if err := s.store.SetRunID(run); err != nil {
		return false, err
	}
	s.run = run
	s.links.links = nil
	clear(s.deferredAdopt)
	clear(s.daemonUnowned)
	clear(s.reattachRejected)
	clear(s.pendingClose)
	return true, nil
}

func (s *Service) resync(ctx context.Context, restarted bool) error {
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
	closeErr := s.closePending(ctx, windows)
	if err := s.store.MarkMissingExcept(live); err != nil {
		return errors.Join(closeErr, err, s.seedAttached(ctx))
	}
	var reattachErr error
	if restarted {
		reattachErr = s.reattach(windows)
	}
	return errors.Join(closeErr, reattachErr, s.resolveDeferred(ctx, windows), s.snapshotOwned(windows), s.seedAttached(ctx))
}

func (s *Service) closePending(ctx context.Context, windows []Window) error {
	now := s.now()
	var errs []error
	for id, op := range s.pendingClose {
		w, ok := findWindow(windows, id)
		if !ok {
			continue
		}
		if tabs := savedTabs(w); !op.deleted && len(tabs) > 0 {
			errs = append(errs, s.store.MarkSnapshotted(op.rowID, tabs, now))
		}
		errs = append(errs, s.closeWindow(ctx, id))
	}
	clear(s.pendingClose)
	return errors.Join(errs...)
}

func (s *Service) resolveDeferred(ctx context.Context, windows []Window) error {
	var errs []error
	for id, parent := range s.deferredAdopt {
		if _, ok := findWindow(windows, id); !ok {
			delete(s.deferredAdopt, id)
			continue
		}
		errs = append(errs, s.adoptDeferred(ctx, id, parent, windows))
	}
	return errors.Join(errs...)
}

func (s *Service) reattach(windows []Window) error {
	rows, err := s.store.ListMissing()
	if err != nil || !s.keepRestoring(len(rows)) {
		return err
	}
	owned, err := s.store.OwnedDriverWindowIDs()
	if err != nil {
		return err
	}
	now := s.now()
	matches := matchRestored(rows, windows, owned, reattachThreshold)
	var errs []error
	for windowID, rowID := range matches {
		errs = append(errs, s.store.Reopen(rowID, windowID, now))
	}
	for _, w := range windows {
		if _, matched := matches[w.ID]; !matched && !owned[w.ID] && hasComparableURLs(w) {
			s.reattachRejected[w.ID] = struct{}{}
		}
	}
	s.keepRestoring(len(rows) - len(matches))
	return errors.Join(errs...)
}

func (s *Service) restoring() bool {
	if !s.now().Before(s.restoreUntil) {
		return false
	}
	rows, err := s.store.ListMissing()
	if err != nil {
		return true
	}
	return s.keepRestoring(len(rows))
}

func (s *Service) keepRestoring(missing int) bool {
	if missing > 0 {
		return true
	}
	s.restoreUntil = time.Time{}
	return false
}

func (s *Service) reattachLate(ctx context.Context, windowID string, live []Window) (bool, error) {
	if !s.now().Before(s.restoreUntil) {
		return false, nil
	}
	if _, ok := s.daemonUnowned[windowID]; ok {
		return false, nil
	}
	if _, ok := s.reattachRejected[windowID]; ok {
		return false, nil
	}
	rows, err := s.store.ListMissing()
	if err != nil || !s.keepRestoring(len(rows)) {
		return false, err
	}
	owned, err := s.store.OwnedDriverWindowIDs()
	if err != nil {
		return false, err
	}
	for id := range s.daemonUnowned {
		owned[id] = true
	}
	for id := range s.reattachRejected {
		owned[id] = true
	}
	rowID, ok := matchRestored(rows, live, owned, reattachThreshold)[windowID]
	if !ok {
		return false, nil
	}
	now := s.now()
	if err := s.store.Reopen(rowID, windowID, now); err != nil {
		return false, err
	}
	s.keepRestoring(len(rows) - 1)
	s.signal()
	w, _ := findWindow(live, windowID)
	return true, s.saveTabs(w, now)
}

func findWindow(windows []Window, id string) (Window, bool) {
	for _, w := range windows {
		if w.ID == id {
			return w, true
		}
	}
	return Window{}, false
}

func hasComparableURLs(w Window) bool {
	return len(urlSet(windowURLs(w))) > 0
}

func (s *Service) seedAttached(ctx context.Context) error {
	if s.attached.Load() != 0 {
		return nil
	}
	id, err := s.sessions.Attached(ctx)
	if err != nil {
		return err
	}
	s.attached.CompareAndSwap(0, uint64(id))
	return nil
}

func (s *Service) adopt(ctx context.Context, windowID string) error {
	if _, ok := s.unowned[windowID]; ok {
		delete(s.unowned, windowID)
		return nil
	}
	if _, err := s.store.GetByDriverWindowID(windowID); err == nil {
		return nil
	} else if !errors.Is(err, ErrWindowNotFound) {
		return err
	}
	parent, err := s.parentSession(windowID)
	if err != nil {
		return err
	}
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	if !s.restoring() {
		return s.adoptInto(windowID, parent, live)
	}
	w, _ := findWindow(live, windowID)
	if !hasComparableURLs(w) {
		s.deferredAdopt[windowID] = parent
		return nil
	}
	_, err = s.reattachLate(ctx, windowID, live)
	return err
}

func (s *Service) adoptDeferred(ctx context.Context, windowID string, parent uint, live []Window) error {
	if !s.restoring() {
		delete(s.deferredAdopt, windowID)
		return s.adoptInto(windowID, parent, live)
	}
	w, _ := findWindow(live, windowID)
	if !hasComparableURLs(w) {
		return nil
	}
	delete(s.deferredAdopt, windowID)
	if reattached, err := s.reattachLate(ctx, windowID, live); reattached || err != nil {
		return err
	}
	return s.adoptInto(windowID, parent, live)
}

func (s *Service) parentSession(windowID string) (uint, error) {
	parentID := s.lastFocusedWindowID
	if parentID == windowID {
		parentID = s.prevFocusedWindowID
	}
	parent, err := s.store.GetByDriverWindowID(parentID)
	if errors.Is(err, ErrWindowNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return parent.SessionID, nil
}

func (s *Service) adoptInto(windowID string, sessionID uint, live []Window) error {
	if sessionID == 0 {
		return nil
	}
	now := s.now()
	if err := s.store.Add(&BrowserWindow{SessionID: sessionID, State: WindowOpen, DriverWindowID: windowID, LastFocusedAt: now}); err != nil {
		return err
	}
	s.signal()
	if w, ok := findWindow(live, windowID); ok {
		return s.saveTabs(w, now)
	}
	return nil
}
