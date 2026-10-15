package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserSessions_OpenRowUsesLiveTabs(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("https://a.example.com", "chrome://newtab/")
	h.driver.SetFocused(w.ID)
	h.driver.SetTitle(w.ID, "New Tab")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID, Tabs: []SavedTab{{URL: "https://stale.example.com"}}}))

	view := h.svc.BrowserSessions(context.Background())

	assert.True(t, view.Connected)
	assert.False(t, view.Outdated)
	assert.Equal(t, map[uint]SessionBrowser{
		1: {State: "open", TabCount: 2, Windows: []WindowView{{
			ID: w.ID, State: "open", Focused: true,
			Tabs: []TabView{
				{ID: w.Tabs[0].ID, URL: "https://a.example.com"},
				{ID: w.Tabs[1].ID, URL: "chrome://newtab/", Title: "New Tab", Active: true},
			},
		}}},
	}, view.Sessions)
}

func TestBrowserSessions_MissingAndSnapshottedRowsUseSavedTabs(t *testing.T) {
	h := newHarness(t)
	live := h.driver.AddWindow("https://live.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowSnapshotted, Tabs: []SavedTab{{URL: "https://s1.example.com", Title: "S1", Active: true}}}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: live.ID}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowSnapshotted, Tabs: []SavedTab{{URL: "https://s2.example.com"}}}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowMissing, Tabs: []SavedTab{{URL: "https://m2.example.com", Title: "M2"}, {URL: "https://m2b.example.com", Active: true}}}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowSnapshotted}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 4, State: WindowSnapshotted, Tabs: []SavedTab{{URL: "https://gone.example.com"}}}))
	require.NoError(t, h.store.DeleteBySession(4))

	view := h.svc.BrowserSessions(context.Background())

	assert.Equal(t, map[uint]SessionBrowser{
		1: {State: "open", TabCount: 2, Windows: []WindowView{
			{ID: "", State: "snapshotted", Tabs: []TabView{{URL: "https://s1.example.com", Title: "S1", Active: true}}},
			{ID: live.ID, State: "open", Tabs: []TabView{{ID: live.Tabs[0].ID, URL: "https://live.example.com", Active: true}}},
		}},
		2: {State: "missing", TabCount: 3, Windows: []WindowView{
			{ID: "", State: "snapshotted", Tabs: []TabView{{URL: "https://s2.example.com"}}},
			{ID: "", State: "missing", Tabs: []TabView{{URL: "https://m2.example.com", Title: "M2"}, {URL: "https://m2b.example.com", Active: true}}},
		}},
		3: {State: "snapshotted", TabCount: 0, Windows: []WindowView{{ID: "", State: "snapshotted", Tabs: []TabView{}}}},
	}, view.Sessions)
}

func TestBrowserSessions_OpenRowFallsBackToSavedTabsWhenNotLive(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "w99", Tabs: []SavedTab{{URL: "https://saved.example.com", Active: true}}}))

	view := h.svc.BrowserSessions(context.Background())

	assert.Equal(t, []WindowView{{ID: "w99", State: "open", Tabs: []TabView{{URL: "https://saved.example.com", Active: true}}}}, view.Sessions[1].Windows)
}

func TestBrowserSessions_DisconnectedUsesSavedTabs(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("https://live.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID, Tabs: []SavedTab{{URL: "https://saved.example.com"}}}))
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	view := h.svc.BrowserSessions(context.Background())

	assert.False(t, view.Connected)
	assert.Equal(t, SessionBrowser{State: "open", TabCount: 1, Windows: []WindowView{{ID: w.ID, State: "open", Tabs: []TabView{{URL: "https://saved.example.com"}}}}}, view.Sessions[1])
}

func TestBrowserSessions_ListErrorFallsBackToSavedTabs(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("https://live.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID, Tabs: []SavedTab{{URL: "https://saved.example.com"}}}))
	h.driver.ListErr = errors.New("boom")

	view := h.svc.BrowserSessions(context.Background())

	assert.True(t, view.Connected)
	assert.Equal(t, []TabView{{URL: "https://saved.example.com"}}, view.Sessions[1].Windows[0].Tabs)
}

func TestBrowserSessions_EmptyWhenNoRows(t *testing.T) {
	h := newHarness(t)
	view := h.svc.BrowserSessions(context.Background())
	assert.NotNil(t, view.Sessions)
	assert.Empty(t, view.Sessions)
}

func TestBrowserSessions_DoesNotTakeServiceLock(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("https://live.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID}))
	h.svc.mu.Lock()
	defer h.svc.mu.Unlock()

	done := make(chan SessionsView, 1)
	go func() { done <- h.svc.BrowserSessions(context.Background()) }()
	select {
	case view := <-done:
		assert.Equal(t, "open", view.Sessions[1].State)
	case <-time.After(2 * time.Second):
		t.Fatal("BrowserSessions blocked on the service lock")
	}
}

func TestBrowserSessions_ReportsOutdatedDriver(t *testing.T) {
	h := newHarness(t)
	h.driver.SetOutdated(true)
	assert.True(t, h.svc.BrowserSessions(context.Background()).Outdated)
	assert.True(t, h.svc.Outdated())

	h.driver.SetOutdated(false)
	assert.False(t, h.svc.Outdated())
}

func (h placeHarness) ownWindow(t *testing.T, sessionID uint, title, workspace string, urls ...string) (Window, string) {
	t.Helper()
	w := h.driver.AddWindow(urls...)
	h.driver.SetTitle(w.ID, title)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: sessionID, State: WindowOpen, DriverWindowID: w.ID}))
	wmID := "wm-" + w.ID
	h.wm.Add(wmID, title+" - Fake", workspace)
	return w, wmID
}

func activeTab(t *testing.T, d *FakeDriver, windowID string) string {
	t.Helper()
	w, ok := d.Window(windowID)
	require.True(t, ok)
	tab, ok := w.ActiveTab()
	require.True(t, ok)
	return tab.ID
}

func TestFocusTab_AttachedSessionActivatesTabAndFocuses(t *testing.T) {
	h := newPlaceHarness(t)
	w, wmID := h.ownWindow(t, 1, "a", "2", "https://a.example.com", "https://b.example.com")
	h.sessions.AttachedID = 1
	h.svc.SetAttached(1)
	drain(h.svc)
	var browserFocusedFirst []string
	h.wm.OnFocus = func(string) { browserFocusedFirst = h.driver.Focused() }

	res, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: w.Tabs[0].ID})
	require.NoError(t, err)
	assert.Equal(t, FocusResult{WindowID: w.ID, TabID: w.Tabs[0].ID, Focused: true}, res)
	assert.Equal(t, w.Tabs[0].ID, activeTab(t, h.driver, w.ID))
	assert.Equal(t, []string{w.ID}, browserFocusedFirst)
	assert.Equal(t, []string{wmID}, h.wm.Focused())
	assert.Empty(t, h.sessions.Activated)
	assert.Empty(t, h.wm.Moves())
}

func TestFocusTab_UnattachedSessionActivatesPlacesAndFocuses(t *testing.T) {
	h := newPlaceHarness(t)
	a, aWM := h.ownWindow(t, 1, "a", "P", "https://a.example.com", "https://a2.example.com")
	_, bWM := h.ownWindow(t, 2, "b", "2", "https://b.example.com")
	h.sessions.AttachedID = 2
	h.svc.SetAttached(2)
	drain(h.svc)
	workspaceAtBrowserFocus, workspaceAtWMFocus := "", ""
	h.driver.OnFocus = func(string) { workspaceAtBrowserFocus = h.wm.Workspace(aWM) }
	h.wm.OnFocus = func(string) { workspaceAtWMFocus = h.wm.Workspace(aWM) }

	res, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: a.ID, TabID: a.Tabs[0].ID})
	require.NoError(t, err)
	assert.True(t, res.Activated)
	assert.True(t, res.Focused)
	assert.Equal(t, []uint{1}, h.sessions.Activated)
	assert.Equal(t, uint64(1), h.svc.attached.Load())
	assert.Equal(t, a.Tabs[0].ID, activeTab(t, h.driver, a.ID))
	assert.Equal(t, "2", workspaceAtBrowserFocus)
	assert.Equal(t, "2", workspaceAtWMFocus)
	assert.Equal(t, "P", h.wm.Workspace(bWM))
	assert.Equal(t, []string{aWM}, h.wm.Focused())
}

func TestFocusTab_StaleAttachedPlacesBeforeFocusing(t *testing.T) {
	h := newPlaceHarness(t)
	_, aWM := h.ownWindow(t, 1, "a", "2", "https://a.example.com")
	b, bWM := h.ownWindow(t, 2, "b", "P", "https://b.example.com")
	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	drain(h.svc)
	h.sessions.AttachedID = 2
	workspaceAtFocus := ""
	h.wm.OnFocus = func(string) { workspaceAtFocus = h.wm.Workspace(bWM) }

	res, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 2, WindowID: b.ID, TabID: b.Tabs[0].ID})
	require.NoError(t, err)
	assert.False(t, res.Activated)
	assert.Equal(t, "2", workspaceAtFocus)
	assert.Equal(t, "P", h.wm.Workspace(aWM))
	assert.Equal(t, uint64(2), h.svc.attached.Load())
}

func TestFocusTab_ParkedWindowThatCannotMoveIsNotWMFocused(t *testing.T) {
	h := newPlaceHarness(t)
	w, _ := h.ownWindow(t, 1, "a", "P", "https://a.example.com")
	h.sessions.AttachedID = 1
	h.svc.SetAttached(1)
	drain(h.svc)
	h.wm.MoveErr = errors.New("aerospace broke")

	res, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: w.Tabs[0].ID})
	require.NoError(t, err)
	assert.True(t, res.Focused)
	assert.Empty(t, h.wm.Focused())
}

func TestFocusTab_RejectsWindowNotOwnedBySession(t *testing.T) {
	h := newPlaceHarness(t)
	other, _ := h.ownWindow(t, 2, "b", "2", "https://b.example.com")
	personal := h.driver.AddWindow("https://personal.example.com")
	h.sessions.AttachedID = 2
	h.svc.SetAttached(2)
	drain(h.svc)

	for _, w := range []Window{other, personal} {
		_, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: w.Tabs[0].ID})
		var appErr *common.AppError
		require.ErrorAs(t, err, &appErr)
		assert.Equal(t, common.CategoryInvalidRequest, appErr.Category)
	}
	assert.Empty(t, h.sessions.Activated)
	assert.Equal(t, uint64(2), h.svc.attached.Load())
	assert.Empty(t, h.driver.Focused())
	assert.Empty(t, h.wm.Focused())
}

func TestFocusTab_RejectsTabNotInWindow(t *testing.T) {
	h := newPlaceHarness(t)
	w, _ := h.ownWindow(t, 1, "a", "2", "https://a.example.com")
	other := h.driver.AddWindow("https://other.example.com")
	h.sessions.AttachedID = 2
	h.svc.SetAttached(2)

	_, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: other.Tabs[0].ID})
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryInvalidRequest, appErr.Category)
	assert.Equal(t, other.Tabs[0].ID, activeTab(t, h.driver, other.ID))
	assert.Empty(t, h.driver.Focused())
	assert.Empty(t, h.sessions.Activated)
	assert.Equal(t, uint64(2), h.svc.attached.Load())
}

func TestFocusTab_OwnedWindowNotLiveIsNotFound(t *testing.T) {
	h := newPlaceHarness(t)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "w99"}))
	h.sessions.AttachedID = 1

	_, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: "w99", TabID: "t1"})
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestFocusTab_UnavailableWhenDisconnected(t *testing.T) {
	h := newPlaceHarness(t)
	w, _ := h.ownWindow(t, 1, "a", "2", "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	_, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: w.Tabs[0].ID})
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.Empty(t, h.sessions.Activated)
}

func TestFocusTab_UnknownSessionIsNotFound(t *testing.T) {
	h := newPlaceHarness(t)
	w, _ := h.ownWindow(t, 1, "a", "2", "https://a.example.com")
	h.sessions.IsAttachedErr = ErrSessionNotFound

	_, err := h.svc.FocusTab(context.Background(), FocusRequest{SessionID: 1, WindowID: w.ID, TabID: w.Tabs[0].ID})
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryNotFound, appErr.Category)
	assert.Empty(t, h.driver.Focused())
}
