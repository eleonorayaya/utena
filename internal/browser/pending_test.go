package browser

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newPendingHarness(t *testing.T) harness {
	t.Helper()
	h := newHarness(t)
	h.driver.Caps = Capabilities{CreateTakesFocus: true, LoadTakesFocus: true}
	h.sessions.AttachedID = 1
	return h
}

func TestSavedTab_StateIn(t *testing.T) {
	assert.Equal(t, TabOpen, SavedTab{}.StateIn(WindowOpen))
	assert.Equal(t, TabSaved, SavedTab{}.StateIn(WindowMissing))
	assert.Equal(t, TabSaved, SavedTab{State: TabOpen}.StateIn(WindowSnapshotted))
	assert.Equal(t, TabPending, SavedTab{State: TabPending}.StateIn(WindowOpen))
	assert.Equal(t, TabPending, SavedTab{State: TabPending}.StateIn(WindowSnapshotted))
}

func TestMergeTabs_KeepsPendingTabsNoLiveTabMatches(t *testing.T) {
	w := Window{Tabs: []Tab{{URL: "https://a.example.com"}, {URL: prURL}}}
	saved := []SavedTab{{URL: "https://old.example.com"}, {URL: prURL, State: TabPending}, {URL: "https://b.example.com", State: TabPending}}

	tabs, fresh := mergeTabs(w, saved)

	assert.True(t, fresh)
	assert.Equal(t, []SavedTab{{URL: "https://a.example.com"}, {URL: prURL}, {URL: "https://b.example.com", State: TabPending}}, tabs)
}

func TestMergeTabs_NoLiveWebTabsKeepsSaved(t *testing.T) {
	saved := []SavedTab{{URL: prURL, State: TabPending}}

	tabs, fresh := mergeTabs(Window{Tabs: []Tab{{URL: "chrome://newtab/"}}}, saved)

	assert.False(t, fresh)
	assert.Equal(t, saved, tabs)
}

func TestOpen_BackgroundSessionHoldsLinkAsPendingTab(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, "https://a.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.Equal(t, OpenResult{WindowID: w.ID, Pending: true}, res)
	live, _ := h.driver.Window(w.ID)
	assert.Equal(t, []string{"https://a.example.com"}, tabURLs(live))
	assert.Equal(t, []SavedTab{{URL: prURL, State: TabPending}}, storedTabs(t, h, 2))
	assert.Empty(t, h.driver.Focused())
}

func TestOpen_BackgroundHoldDedupes(t *testing.T) {
	h := newPendingHarness(t)
	ownWindow(t, h, 2, "https://a.example.com")

	for range 2 {
		_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
		require.NoError(t, err)
	}

	assert.Equal(t, []SavedTab{{URL: prURL, State: TabPending}}, storedTabs(t, h, 2))
}

func TestOpen_BackgroundHoldActivatesAnOpenTabWithoutFocus(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, prURL, "https://a.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.Equal(t, OpenResult{WindowID: w.ID, TabID: w.Tabs[0].ID}, res)
	live, _ := h.driver.Window(w.ID)
	active, _ := live.ActiveTab()
	assert.Equal(t, w.Tabs[0].ID, active.ID)
	assert.Empty(t, h.driver.Focused())
	assert.Empty(t, storedTabs(t, h, 2))
}

func TestOpen_BackgroundHoldIntoTheFocusedWindowLoadsNow(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, "https://a.example.com")
	h.driver.SetFocused(w.ID)

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.False(t, res.Pending)
	assert.Equal(t, w.ID, res.WindowID)
	live, _ := h.driver.Window(w.ID)
	assert.Equal(t, []string{"https://a.example.com", prURL}, tabURLs(live))
	assert.Empty(t, storedTabs(t, h, 2))
	assert.Empty(t, h.driver.Focused())
}

func TestOpen_BackgroundHoldWithoutWindowAddsSavedRow(t *testing.T) {
	h := newPendingHarness(t)

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.True(t, res.Pending)
	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, live)
	rows := rowsOf(t, h, 2)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []SavedTab{{URL: prURL, State: TabPending}}, rows[0].Tabs)
}

func TestOpen_BackgroundHoldPrefersMostRecentRestorableRow(t *testing.T) {
	h := newPendingHarness(t)
	older := addSnapshotted(t, h, 2, time.Unix(100, 0), SavedTab{URL: "https://old.example.com"})
	newer := addSnapshotted(t, h, 2, time.Unix(200, 0), SavedTab{URL: "https://new.example.com"})

	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	got, err := h.store.Get(newer.ID)
	require.NoError(t, err)
	assert.Equal(t, []SavedTab{{URL: "https://new.example.com"}, {URL: prURL, State: TabPending}}, got.Tabs)
	got, err = h.store.Get(older.ID)
	require.NoError(t, err)
	assert.Equal(t, []SavedTab{{URL: "https://old.example.com"}}, got.Tabs)
}

func TestOpen_BackgroundHoldWorksWhileDisconnected(t *testing.T) {
	h := newPendingHarness(t)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.True(t, res.Pending)
	assert.Equal(t, []SavedTab{{URL: prURL, State: TabPending}}, storedTabs(t, h, 2))
}

func TestOpen_AttachedSessionStillOpensLive(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	assert.False(t, res.Pending)
	live, _ := h.driver.Window(w.ID)
	assert.Equal(t, []string{"https://a.example.com", prURL}, tabURLs(live))
}

func TestOpen_BackgroundWithoutLoadCapabilityOpensLive(t *testing.T) {
	h := newHarness(t)
	h.sessions.AttachedID = 1
	w := ownWindow(t, h, 2, "https://a.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	assert.False(t, res.Pending)
	live, _ := h.driver.Window(w.ID)
	assert.Equal(t, []string{"https://a.example.com", prURL}, tabURLs(live))
}

func TestFocusChanged_LoadsPendingTabs(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, "https://a.example.com")
	for _, u := range []string{prURL, "https://b.example.com"} {
		_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: u})
		require.NoError(t, err)
	}

	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: w.ID})

	live, _ := h.driver.Window(w.ID)
	assert.Equal(t, []string{"https://a.example.com", prURL, "https://b.example.com"}, tabURLs(live))
	assert.Empty(t, pendingTabs(storedTabs(t, h, 2)))
	assert.Len(t, storedTabs(t, h, 2), 3)
}

func TestFocusChanged_FailedLoadStaysPending(t *testing.T) {
	h := newPendingHarness(t)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: "gone", Tabs: []SavedTab{{URL: prURL, State: TabPending}}}))

	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: "gone"})

	assert.Equal(t, []SavedTab{{URL: prURL, State: TabPending}}, storedTabs(t, h, 2))
}

func TestSnapshot_KeepsPendingTabs(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, "https://a.example.com")
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	require.NoError(t, h.svc.snapshot(context.Background(), w.ID))

	assert.Equal(t, []SavedTab{{URL: "https://a.example.com", Active: true}, {URL: prURL, State: TabPending}}, storedTabs(t, h, 2))
}

func TestArchive_KeepsPendingTabsAndRestoreLoadsThem(t *testing.T) {
	h := newPendingHarness(t)
	ownWindow(t, h, 2, "https://a.example.com")
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)

	require.NoError(t, h.svc.Archive(context.Background(), 2))
	rows := rowsOf(t, h, 2)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []SavedTab{{URL: "https://a.example.com", Active: true}, {URL: prURL, State: TabPending}}, rows[0].Tabs)

	h.svc.SetAttached(2)
	require.NoError(t, h.svc.place(context.Background()))

	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, []string{"https://a.example.com", prURL}, tabURLs(live[0]))
	assert.Empty(t, pendingTabs(storedTabs(t, h, 2)))
}

func TestMatchRestored_IgnoresPendingTabs(t *testing.T) {
	rows := []BrowserWindow{{Model: gorm.Model{ID: 1}, State: WindowMissing, Tabs: []SavedTab{
		{URL: "https://a.example.com"},
		{URL: "https://b.example.com", State: TabPending},
		{URL: "https://c.example.com", State: TabPending},
	}}}
	windows := []Window{{ID: "w1", Tabs: []Tab{{URL: "https://a.example.com"}}}}

	assert.Equal(t, map[string]uint{"w1": 1}, matchRestored(rows, windows, map[string]bool{}, reattachThreshold))
}

func TestBrowserSessions_ShowsPendingTabs(t *testing.T) {
	h := newPendingHarness(t)
	w := ownWindow(t, h, 2, "https://a.example.com")
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL})
	require.NoError(t, err)
	_, err = h.svc.Open(context.Background(), OpenRequest{SessionID: 3, URL: prURL})
	require.NoError(t, err)

	view := h.svc.BrowserSessions(context.Background())

	assert.Equal(t, []TabView{
		{ID: w.Tabs[0].ID, URL: "https://a.example.com", Active: true, State: TabOpen},
		{URL: prURL, State: TabPending},
	}, view.Sessions[2].Windows[0].Tabs)
	assert.Equal(t, SessionBrowser{State: "snapshotted", TabCount: 1, Windows: []WindowView{{State: "snapshotted", Tabs: []TabView{{URL: prURL, State: TabPending}}}}}, view.Sessions[3])
}
