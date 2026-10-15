package browser

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeTimer struct {
	d       time.Duration
	f       func()
	stopped bool
	fired   bool
}

type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

func (c *fakeClock) afterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{d: d, f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		live := !t.stopped && !t.fired
		t.stopped = true
		return live
	}
}

func (c *fakeClock) live() []*fakeTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			out = append(out, t)
		}
	}
	return out
}

func (c *fakeClock) fire() int {
	c.mu.Lock()
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
	return len(due)
}

func ownWindow(t *testing.T, h harness, sessionID uint, urls ...string) Window {
	t.Helper()
	w := h.driver.AddWindow(urls...)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: sessionID, State: WindowOpen, DriverWindowID: w.ID}))
	return w
}

func storedTabs(t *testing.T, h harness, sessionID uint) []SavedTab {
	t.Helper()
	rows, err := h.store.ListBySession(sessionID)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0].Tabs
}

func urlsOf(tabs []SavedTab) []string {
	out := make([]string, 0, len(tabs))
	for _, t := range tabs {
		out = append(out, t.URL)
	}
	return out
}

func TestTabsChanged_BurstSavesOnceWithLatestTabs(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	_, err := h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	_, err = h.driver.OpenTab(context.Background(), w.ID, "https://c.example.com")
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	assert.Empty(t, storedTabs(t, h, 1))
	require.Len(t, h.clock.live(), 1)
	assert.Equal(t, snapshotDelay, h.clock.live()[0].d)
	assert.Equal(t, 1, h.clock.fire())

	tabs := storedTabs(t, h, 1)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}, urlsOf(tabs))
	assert.True(t, tabs[2].Active)
}

func TestTabsChanged_SnapshotsAgainAfterFiring(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	h.clock.fire()
	_, err := h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	assert.Equal(t, 1, h.clock.fire())

	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, urlsOf(storedTabs(t, h, 1)))
}

func TestTabsChanged_UnownedWindowNeverSaved(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("https://a.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	assert.Empty(t, h.clock.live())
	assert.Equal(t, 0, h.clock.fire())
}

func TestTabsChanged_DisconnectedServiceDoesNotSave(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	h.clock.fire()

	assert.Empty(t, storedTabs(t, h, 1))
}

func TestTabsChanged_WindowGoneKeepsSnapshot(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	require.NotEmpty(t, storedTabs(t, h, 1))

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	h.driver.RemoveWindow(w.ID)
	h.clock.fire()

	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(storedTabs(t, h, 1)))
}

func TestResync_SavesOwnedLiveWindows(t *testing.T) {
	h := newHarness(t)
	owned := ownWindow(t, h, 1, "https://a.example.com", "https://b.example.com")
	h.driver.AddWindow("https://personal.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	rows, err := h.store.ListBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, owned.ID, rows[0].DriverWindowID)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, urlsOf(rows[0].Tabs))
	assert.False(t, rows[0].SnapshotAt.IsZero())
}

func TestOpen_CreatedWindowStoresInitialTabs(t *testing.T) {
	h := newHarness(t)

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	require.True(t, res.Created)

	rows, err := h.store.ListBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, []string{prURL}, urlsOf(rows[0].Tabs))
	assert.False(t, rows[0].SnapshotAt.IsZero())
}

func TestWindowCreated_AdoptedWindowStoresInitialTabs(t *testing.T) {
	h := newHarness(t)
	parent := ownWindow(t, h, 3, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})

	child := h.driver.AddWindow("chrome://newtab", "https://b.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: child.ID})

	got, err := h.store.GetByDriverWindowID(child.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://b.example.com"}, urlsOf(got.Tabs))
	assert.False(t, got.SnapshotAt.IsZero())
}

func TestSnapshot_NewTabOnlyWindowKeepsSavedTabs(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("chrome://newtab")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID, Tabs: []SavedTab{{URL: "https://b.example.com"}}}))

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	assert.Equal(t, 1, h.clock.fire())
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	assert.Equal(t, []string{"https://b.example.com"}, urlsOf(storedTabs(t, h, 1)))
}

func removeWindow(h harness, id string) {
	h.driver.RemoveWindow(id)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: id})
}

func TestWindowRemoved_GraceExpiryDeletesRow(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	removeWindow(h, res.WindowID)

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, h.clock.live(), 1)
	assert.Equal(t, removeGrace, h.clock.live()[0].d)

	assert.Equal(t, 1, h.clock.fire())

	all, err := h.store.ListBySession(1)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestWindowRemoved_DisconnectInsideGraceMarksMissingAndKeepsTabs(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	removeWindow(h, res.WindowID)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	assert.Empty(t, h.clock.live())
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	require.Len(t, missing, 1)
	assert.Equal(t, uint(1), missing[0].SessionID)
	assert.Empty(t, missing[0].DriverWindowID)
	assert.Equal(t, []string{prURL}, urlsOf(missing[0].Tabs))

	h.clock.fire()
	missing, err = h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestWindowRemoved_UnownedWindowIsNoop(t *testing.T) {
	h := newHarness(t)
	owned := ownWindow(t, h, 1, "https://a.example.com")
	personal := h.driver.AddWindow("https://personal.example.com")

	removeWindow(h, personal.ID)

	assert.Empty(t, h.clock.live())
	got, err := h.store.GetByDriverWindowID(owned.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
}

func TestWindowRemoved_DropsLinkAndPendingSnapshot(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.links.links = map[string]string{w.ID: "wm-1"}
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	removeWindow(h, w.ID)

	_, linked := h.svc.links.links[w.ID]
	assert.False(t, linked)
	require.Len(t, h.clock.live(), 1)
	assert.Equal(t, removeGrace, h.clock.live()[0].d)
}

func TestWindowRemoved_ReconnectDoesNotResurrectDeletedRows(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	removeWindow(h, res.WindowID)
	h.clock.fire()

	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	all, err := h.store.ListBySession(1)
	require.NoError(t, err)
	assert.Empty(t, all)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestWindowRemoved_StaleGraceTimerDoesNotDeleteReusedID(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	removeWindow(h, res.WindowID)
	stale := h.clock.live()[0]

	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: res.WindowID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: res.WindowID})

	stale.f()

	got, err := h.store.GetByDriverWindowID(res.WindowID)
	require.NoError(t, err)
	assert.Equal(t, uint(2), got.SessionID)
}

func TestWindowRemoved_ConnectedInsideGraceFlushesPendingRemoval(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	removeWindow(h, res.WindowID)

	connectRun(h, "run-b")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: res.WindowID}))
	h.clock.fire()

	got, err := h.store.GetByDriverWindowID(res.WindowID)
	require.NoError(t, err)
	assert.Equal(t, uint(2), got.SessionID)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	require.Len(t, missing, 1)
	assert.Equal(t, uint(1), missing[0].SessionID)
	assert.Equal(t, []string{prURL}, urlsOf(missing[0].Tabs))
}

func TestWindowRemoved_DuplicateEventKeepsOneTimer(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	removeWindow(h, res.WindowID)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: res.WindowID})

	require.Len(t, h.clock.live(), 1)
	assert.Equal(t, 1, h.clock.fire())
	all, err := h.store.ListBySession(1)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestDisconnected_StopsPendingSnapshots(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	require.Len(t, h.clock.live(), 1)

	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	assert.Empty(t, h.clock.live())
	assert.Empty(t, h.svc.snapshots)
}

func TestOnAppEnd_DisconnectsAndStopsTimers(t *testing.T) {
	h := newHarness(t)
	snap := ownWindow(t, h, 1, "https://a.example.com")
	gone := ownWindow(t, h, 2, "https://b.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: snap.ID})
	removeWindow(h, gone.ID)
	require.Len(t, h.clock.live(), 2)

	require.NoError(t, h.svc.OnAppEnd(context.Background()))

	assert.False(t, h.svc.Connected())
	assert.Empty(t, h.clock.live())
	assert.Empty(t, h.svc.snapshots)
	assert.Empty(t, h.svc.removals)
}

func TestConnected_ReattachesRestoredWindowsAfterChromeRestart(t *testing.T) {
	h := newPlaceHarness(t)
	connectRun(h.harness, "r1")
	oldA := h.own(t, 1, "a", "2")
	oldB := h.own(t, 2, "b", "P")
	connectRun(h.harness, "r1")
	open, err := h.store.ListOpen()
	require.NoError(t, err)
	require.Len(t, open, 2)
	for _, r := range open {
		require.NotEmpty(t, r.Tabs)
	}

	for _, r := range open {
		removeWindow(h.harness, r.DriverWindowID)
	}
	h.wm.Remove(oldA)
	h.wm.Remove(oldB)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	require.Len(t, missing, 2)

	newA := h.driver.AddWindow("https://a.example.com", "https://a2.example.com")
	h.driver.SetTitle(newA.ID, "a")
	h.wm.Add("wm-"+newA.ID, "a - Fake", "P")
	newB := h.driver.AddWindow("https://b.example.com")
	h.driver.SetTitle(newB.ID, "b")
	h.wm.Add("wm-"+newB.ID, "b - Fake", "2")
	personal := h.driver.AddWindow("https://personal.example.com")
	h.driver.SetTitle(personal.ID, "personal")
	h.wm.Add("wm-"+personal.ID, "personal - Fake", "2")
	h.svc.SetAttached(1)
	drain(h.svc)

	connectRun(h.harness, "r2")

	rowsA, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rowsA, 1)
	assert.Equal(t, newA.ID, rowsA[0].DriverWindowID)
	assert.Equal(t, []string{"https://a.example.com", "https://a2.example.com"}, urlsOf(rowsA[0].Tabs))
	rowsB, err := h.store.ListOpenBySession(2)
	require.NoError(t, err)
	require.Len(t, rowsB, 1)
	assert.Equal(t, newB.ID, rowsB[0].DriverWindowID)
	missing, err = h.store.ListMissing()
	require.NoError(t, err)
	assert.Empty(t, missing)
	owned, err := h.store.OwnedDriverWindowIDs()
	require.NoError(t, err)
	assert.False(t, owned[personal.ID])

	select {
	case <-h.svc.swapCh:
	default:
		t.Fatal("placement not signalled")
	}
	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "2", h.wm.Workspace("wm-"+newA.ID))
	assert.Equal(t, "P", h.wm.Workspace("wm-"+newB.ID))
	assert.Equal(t, "2", h.wm.Workspace("wm-"+personal.ID))
}

func manualNow(h harness) *time.Time {
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	h.svc.now = func() time.Time { return now }
	return &now
}

func addMissing(t *testing.T, h harness, sessionID uint, urls ...string) {
	t.Helper()
	row := &BrowserWindow{SessionID: sessionID, State: WindowMissing}
	for _, u := range urls {
		row.Tabs = append(row.Tabs, SavedTab{URL: u})
	}
	require.NoError(t, h.store.Add(row))
}

func focusedParent(t *testing.T, h harness, sessionID uint) Window {
	t.Helper()
	parent := ownWindow(t, h, sessionID, "https://parent.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})
	return parent
}

func TestConnected_SameRunReconnectDoesNotReattach(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com")
	w := h.driver.AddWindow("https://a.example.com")

	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	connectRun(h, "r1")

	owned, err := h.store.OwnedDriverWindowIDs()
	require.NoError(t, err)
	assert.False(t, owned[w.ID])
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestWindowCreated_LateRestoredWindowReattachesBeforeAdoption(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")
	focusedParent(t, h, 1)
	*now = now.Add(restoreWindow - time.Second)
	drain(h.svc)

	w := h.driver.AddWindow("https://a.example.com", "https://b.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})

	got, err := h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(2), got.SessionID)
	assert.Equal(t, *now, got.SnapshotAt.UTC())
	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Empty(t, missing)
	select {
	case <-h.svc.swapCh:
	default:
		t.Fatal("placement not signalled")
	}
}

func TestTabsChanged_LateRestoredWindowReattachesOnceTabsLoad(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")

	w := h.driver.AddWindow()
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})
	_, err := h.store.GetByDriverWindowID(w.ID)
	require.ErrorIs(t, err, ErrWindowNotFound)

	_, err = h.driver.OpenTab(context.Background(), w.ID, "https://a.example.com")
	require.NoError(t, err)
	_, err = h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	got, err := h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(2), got.SessionID)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, urlsOf(got.Tabs))
}

func TestWindowCreated_AfterRestoreWindowAdoptsNormally(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com")
	focusedParent(t, h, 1)
	*now = now.Add(restoreWindow)

	w := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})

	got, err := h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)

	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})
	got, err = h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
}

func TestTabsChanged_AfterRestoreWindowDoesNotReattach(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com")
	*now = now.Add(restoreWindow)

	w := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	_, err := h.store.GetByDriverWindowID(w.ID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestWindowCreated_SameRunReconnectDoesNotOpenRestoreWindow(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	connectRun(h, "r1")
	*now = now.Add(restoreWindow)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	connectRun(h, "r1")
	addMissing(t, h, 2, "https://a.example.com")

	w := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	_, err := h.store.GetByDriverWindowID(w.ID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestConnected_NewRunDropsCachedLinks(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	h.svc.links.links = map[string]string{"w1": "wm-1"}

	connectRun(h, "r2")

	assert.Empty(t, h.svc.links.links)
}
