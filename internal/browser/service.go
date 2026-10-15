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
	restoreWindow   = 2 * time.Minute
)

type pendingTimer struct {
	stop func() bool
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
	snapshots           map[string]*pendingTimer
	removals            map[string]*pendingTimer
	restoreUntil        time.Time
	deferredAdopt       map[string]uint
	daemonUnowned       map[string]struct{}
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

		deferredAdopt: map[string]uint{},
		daemonUnowned: map[string]struct{}{},

		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		snapshotDelay: snapshotDelay,
		removeGrace:   removeGrace,
		restoreWindow: restoreWindow,
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
			s.linkCreated(ctx, w.ID, before, live)
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

func (s *Service) linkCreated(ctx context.Context, windowID string, before []wm.Window, live []Window) {
	deadline := time.Now().Add(createdLinkWait)
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
	return nil
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
	if !s.connected.Load() || s.wmInert {
		return nil
	}
	attached := uint(s.attached.Load())

	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
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
	s.placedFor = attached
	return nil
}

func (s *Service) ensurePlacedLocked(ctx context.Context) {
	if s.placedFor == uint(s.attached.Load()) {
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
		if restarted {
			s.restoreUntil = s.now().Add(s.restoreWindow)
		}
		err = errors.Join(flushErr, runErr, s.resync(ctx, restarted))
		s.signal()
	case EventDisconnected:
		s.connected.Store(false)
		err = s.flushRemovals()
		stopTimers(s.snapshots)
		clear(s.deferredAdopt)
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
		return s.adoptDeferred(ctx, windowID, parent)
	}
	if _, err := s.store.GetByDriverWindowID(windowID); errors.Is(err, ErrWindowNotFound) {
		if _, ok := s.unowned[windowID]; ok {
			return nil
		}
		_, err := s.reattachLate(ctx, windowID)
		return err
	} else if err != nil {
		return err
	}
	s.scheduleSnapshot(windowID)
	if _, ok := s.links.links[windowID]; !ok {
		s.signal()
	}
	return nil
}

func (s *Service) windowRemoved(windowID string) error {
	delete(s.links.links, windowID)
	delete(s.deferredAdopt, windowID)
	delete(s.daemonUnowned, windowID)
	stopTimer(s.snapshots, windowID)
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
	})
}

func (s *Service) snapshot(ctx context.Context, windowID string) error {
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	for _, w := range live {
		if w.ID == windowID {
			return s.saveTabs(w, s.now())
		}
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
	if err := s.store.MarkMissingExcept(live); err != nil {
		return errors.Join(err, s.seedAttached(ctx))
	}
	var reattachErr error
	if restarted {
		reattachErr = s.reattach(windows)
	}
	return errors.Join(reattachErr, s.snapshotOwned(windows), s.seedAttached(ctx))
}

func (s *Service) reattach(windows []Window) error {
	rows, err := s.store.ListMissing()
	if err != nil || len(rows) == 0 {
		return err
	}
	owned, err := s.store.OwnedDriverWindowIDs()
	if err != nil {
		return err
	}
	now := s.now()
	var errs []error
	for windowID, rowID := range matchRestored(rows, windows, owned, reattachThreshold) {
		errs = append(errs, s.store.Reopen(rowID, windowID, now))
	}
	return errors.Join(errs...)
}

func (s *Service) restoring() bool {
	return s.now().Before(s.restoreUntil)
}

func (s *Service) reattachLate(ctx context.Context, windowID string) (bool, error) {
	if !s.restoring() {
		return false, nil
	}
	if _, ok := s.daemonUnowned[windowID]; ok {
		return false, nil
	}
	rows, err := s.store.ListMissing()
	if err != nil || len(rows) == 0 {
		return false, err
	}
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return false, err
	}
	owned, err := s.store.OwnedDriverWindowIDs()
	if err != nil {
		return false, err
	}
	for id := range s.daemonUnowned {
		owned[id] = true
	}
	rowID, ok := matchRestored(rows, live, owned, reattachThreshold)[windowID]
	if !ok {
		return false, nil
	}
	if err := s.store.Reopen(rowID, windowID, s.now()); err != nil {
		return false, err
	}
	s.signal()
	return true, s.snapshot(ctx, windowID)
}

func (s *Service) hasComparableURLs(ctx context.Context, windowID string) (bool, error) {
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return false, err
	}
	for _, w := range live {
		if w.ID == windowID {
			return len(urlSet(windowURLs(w))) > 0, nil
		}
	}
	return false, nil
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
	if s.restoring() {
		comparable, err := s.hasComparableURLs(ctx, windowID)
		if err != nil {
			return err
		}
		if !comparable {
			s.deferredAdopt[windowID] = parent
			return nil
		}
		if reattached, err := s.reattachLate(ctx, windowID); reattached || err != nil {
			return err
		}
	}
	return s.adoptInto(ctx, windowID, parent)
}

func (s *Service) adoptDeferred(ctx context.Context, windowID string, parent uint) error {
	comparable, err := s.hasComparableURLs(ctx, windowID)
	if err != nil || !comparable {
		return err
	}
	delete(s.deferredAdopt, windowID)
	if reattached, err := s.reattachLate(ctx, windowID); reattached || err != nil {
		return err
	}
	return s.adoptInto(ctx, windowID, parent)
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

func (s *Service) adoptInto(ctx context.Context, windowID string, sessionID uint) error {
	if sessionID == 0 {
		return nil
	}
	if err := s.store.Add(&BrowserWindow{SessionID: sessionID, State: WindowOpen, DriverWindowID: windowID, LastFocusedAt: s.now()}); err != nil {
		return err
	}
	s.signal()
	return s.snapshot(ctx, windowID)
}
