package browser

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testRestoreWindow = 2 * time.Minute

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
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
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
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)
	*now = now.Add(testRestoreWindow - time.Second)
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
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")
	connectRun(h, "r1")

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
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)
	*now = now.Add(testRestoreWindow)

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
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	*now = now.Add(testRestoreWindow)

	w := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	_, err := h.store.GetByDriverWindowID(w.ID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestWindowCreated_SameRunReconnectDoesNotOpenRestoreWindow(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	connectRun(h, "r1")
	*now = now.Add(testRestoreWindow)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")

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

func createEmptyWindow(h harness) Window {
	w := h.driver.AddWindow("chrome://newtab/")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})
	return w
}

func loadTabs(t *testing.T, h harness, windowID string, urls ...string) {
	t.Helper()
	for _, u := range urls {
		_, err := h.driver.OpenTab(context.Background(), windowID, u)
		require.NoError(t, err)
	}
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: windowID})
}

func sessionOf(t *testing.T, h harness, windowID string) uint {
	t.Helper()
	got, err := h.store.GetByDriverWindowID(windowID)
	if errors.Is(err, ErrWindowNotFound) {
		return 0
	}
	require.NoError(t, err)
	return got.SessionID
}

func TestWindowCreated_EmptyWhileRestoringDefersAdoptionUntilReattached(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)
	assert.Equal(t, uint(0), sessionOf(t, h, w.ID))

	loadTabs(t, h, w.ID, "https://a.example.com", "https://b.example.com")

	assert.Equal(t, uint(2), sessionOf(t, h, w.ID))
	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Empty(t, h.svc.deferredAdopt)
}

func TestWindowCreated_EmptyWhileRestoringFallsBackToRecordedParent(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)
	focusedParent(t, h, 3)
	drain(h.svc)
	loadTabs(t, h, w.ID, "https://x.example.com")

	got, err := h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
	assert.Equal(t, []string{"https://x.example.com"}, urlsOf(got.Tabs))
	assert.Empty(t, h.svc.deferredAdopt)
	select {
	case <-h.svc.swapCh:
	default:
		t.Fatal("placement not signalled")
	}
}

func TestWindowCreated_DeferredWindowWaitsWhileStillEmpty(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	assert.Equal(t, uint(0), sessionOf(t, h, w.ID))
	assert.Contains(t, h.svc.deferredAdopt, w.ID)
}

func TestWindowCreated_DeferredWindowFallsBackToParentAfterRestoreWindow(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)
	*now = now.Add(testRestoreWindow)
	loadTabs(t, h, w.ID, "https://a.example.com")

	assert.Equal(t, uint(1), sessionOf(t, h, w.ID))
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestWindowCreated_EmptyOutsideRestoreWindowAdoptsImmediately(t *testing.T) {
	h := newHarness(t)
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)

	assert.Equal(t, uint(1), sessionOf(t, h, w.ID))
	assert.Empty(t, h.svc.deferredAdopt)
}

func TestWindowRemoved_ClearsDeferredAdoption(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)
	w := createEmptyWindow(h)
	require.Contains(t, h.svc.deferredAdopt, w.ID)

	removeWindow(h, w.ID)

	assert.Empty(t, h.svc.deferredAdopt)
}

func TestConnected_SameRunReconnectResolvesDeferredWindows(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com", "https://b.example.com")
	addMissing(t, h, 3, "https://z.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)
	loaded := createEmptyWindow(h)
	waiting := createEmptyWindow(h)
	gone := createEmptyWindow(h)
	require.Len(t, h.svc.deferredAdopt, 3)

	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	assert.Len(t, h.svc.deferredAdopt, 3)
	for _, u := range []string{"https://a.example.com", "https://b.example.com"} {
		_, err := h.driver.OpenTab(context.Background(), loaded.ID, u)
		require.NoError(t, err)
	}
	h.driver.RemoveWindow(gone.ID)
	connectRun(h, "r1")

	assert.Equal(t, uint(2), sessionOf(t, h, loaded.ID))
	assert.Equal(t, map[string]uint{waiting.ID: 1}, h.svc.deferredAdopt)
}

func TestReattachLate_WindowRejectedAtConnectedStaysUnowned(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, prURL)
	personal := h.driver.AddWindow("https://mail.example.com")
	connectRun(h, "r1")
	require.Contains(t, h.svc.reattachRejected, personal.ID)

	_, err := h.driver.OpenTab(context.Background(), personal.ID, prURL)
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: personal.ID})

	assert.Equal(t, uint(0), sessionOf(t, h, personal.ID))
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestReattachLate_WindowEmptyAtConnectedCanStillReattach(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, prURL)
	empty := h.driver.AddWindow("chrome://newtab/")
	connectRun(h, "r1")
	require.NotContains(t, h.svc.reattachRejected, empty.ID)

	loadTabs(t, h, empty.ID, prURL)

	assert.Equal(t, uint(2), sessionOf(t, h, empty.ID))
}

func TestReattachRejected_ClearedOnRemovalAndRunChange(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, prURL)
	a := h.driver.AddWindow("https://mail.example.com")
	b := h.driver.AddWindow("https://news.example.com")
	connectRun(h, "r1")
	require.Len(t, h.svc.reattachRejected, 2)

	removeWindow(h, a.ID)
	assert.NotContains(t, h.svc.reattachRejected, a.ID)
	h.driver.RemoveWindow(b.ID)
	connectRun(h, "r2")
	assert.Empty(t, h.svc.reattachRejected)
}

func TestWindowCreated_ComparableNonMatchingWindowStaysUnownedWhileRestoring(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := h.driver.AddWindow("https://x.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})

	assert.Equal(t, uint(0), sessionOf(t, h, w.ID))
	assert.Empty(t, h.svc.deferredAdopt)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestWindowCreated_EmptyAfterRestartWithNothingMissingAdoptsImmediately(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := createEmptyWindow(h)

	assert.Equal(t, uint(1), sessionOf(t, h, w.ID))
	assert.Empty(t, h.svc.deferredAdopt)
	assert.True(t, h.svc.restoreUntil.IsZero())
}

func TestConnected_ReattachingEveryMissingRowEndsRestoreWindow(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	w := h.driver.AddWindow("https://a.example.com")

	connectRun(h, "r1")

	assert.Equal(t, uint(2), sessionOf(t, h, w.ID))
	assert.True(t, h.svc.restoreUntil.IsZero())
}

func TestReattachLate_LastMissingRowEndsRestoreWindow(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	require.False(t, h.svc.restoreUntil.IsZero())

	w := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})

	assert.Equal(t, uint(2), sessionOf(t, h, w.ID))
	assert.True(t, h.svc.restoreUntil.IsZero())
}

func TestTabsChanged_DeferredWindowStillEmptyAfterRestoreWindowAdoptsParent(t *testing.T) {
	h := newHarness(t)
	now := manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)
	w := createEmptyWindow(h)
	require.Contains(t, h.svc.deferredAdopt, w.ID)

	*now = now.Add(testRestoreWindow)
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: w.ID})

	assert.Equal(t, uint(1), sessionOf(t, h, w.ID))
	assert.Empty(t, h.svc.deferredAdopt)
}

func TestSavedTabs_SkipsAboutPages(t *testing.T) {
	w := Window{Tabs: []Tab{{URL: "about:blank"}, {URL: "https://a.example.com"}, {URL: "chrome://newtab/"}}}

	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(savedTabs(w)))
	assert.Empty(t, normalizeURL("about:blank"))
	assert.Empty(t, normalizeURL("About:Blank"))
	assert.Empty(t, urlSet([]string{"about:blank", "chrome://newtab/"}))
}

func TestWindowCreated_AboutBlankWhileRestoringIsDeferred(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 1)

	w := h.driver.AddWindow("about:blank")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: w.ID})

	assert.Contains(t, h.svc.deferredAdopt, w.ID)
}

func TestRestore_ReopenFailureClosesOrphanedWindow(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	fail := false
	require.NoError(t, h.store.db.Model(&BrowserWindow{}).Callback().Update().Before("gorm:update").Register("test:fail_update", func(tx *gorm.DB) {
		if fail {
			_ = tx.AddError(errors.New("store broke"))
		}
	}))
	fail = true

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, live)
	closed := h.driver.Closed()
	require.Len(t, closed, 1)
	assert.Contains(t, h.svc.closing, closed[0])
	fail = false
	restorable, err := h.store.ListRestorable(1)
	require.NoError(t, err)
	assert.Len(t, restorable, 1)

	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: closed[0]})
	assert.Empty(t, h.svc.closing)
	assert.Empty(t, h.clock.live())
}

func TestReattach_NeverClaimsDaemonCreatedUnownedWindows(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 2, prURL)
	connectRun(h, "r1")

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	require.True(t, res.Created)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: res.WindowID})
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: res.WindowID})

	assert.Equal(t, uint(0), sessionOf(t, h, res.WindowID))
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)

	removeWindow(h, res.WindowID)
	assert.Empty(t, h.svc.daemonUnowned)
}

func TestConnected_NewRunClearsDaemonUnownedAndDeferred(t *testing.T) {
	h := newHarness(t)
	h.svc.daemonUnowned["w1"] = struct{}{}
	h.svc.deferredAdopt["w2"] = 1

	connectRun(h, "r1")

	assert.Empty(t, h.svc.daemonUnowned)
	assert.Empty(t, h.svc.deferredAdopt)
}

func addSnapshotted(t *testing.T, h harness, sessionID uint, lastFocused time.Time, tabs ...SavedTab) *BrowserWindow {
	t.Helper()
	row := &BrowserWindow{SessionID: sessionID, State: WindowSnapshotted, Tabs: tabs, LastFocusedAt: lastFocused}
	require.NoError(t, h.store.Add(row))
	return row
}

func rowsOf(t *testing.T, h harness, sessionID uint) []BrowserWindow {
	t.Helper()
	rows, err := h.store.ListBySession(sessionID)
	require.NoError(t, err)
	return rows
}

func tabURLs(w Window) []string {
	out := make([]string, 0, len(w.Tabs))
	for _, t := range w.Tabs {
		out = append(out, t.URL)
	}
	return out
}

func activeURL(t *testing.T, h harness, windowID string) string {
	t.Helper()
	w, ok := h.driver.Window(windowID)
	require.True(t, ok)
	tab, ok := w.ActiveTab()
	require.True(t, ok)
	return tab.URL
}

func TestArchive_SnapshotsClosesAndKeepsRow(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	_, err := h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)
	other := ownWindow(t, h, 2, "https://other.example.com")

	require.NoError(t, h.svc.Archive(context.Background(), 1))

	assert.Equal(t, []string{w.ID}, h.driver.Closed())
	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Empty(t, rows[0].DriverWindowID)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, urlsOf(rows[0].Tabs))
	assert.True(t, rows[0].Tabs[1].Active)
	assert.False(t, rows[0].SnapshotAt.IsZero())
	assert.Equal(t, uint(2), sessionOf(t, h, other.ID))
}

func TestArchive_EmptyWindowKeepsSavedTabs(t *testing.T) {
	h := newHarness(t)
	w := h.driver.AddWindow("chrome://newtab/")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID, Tabs: []SavedTab{{URL: "https://b.example.com"}}}))

	require.NoError(t, h.svc.Archive(context.Background(), 1))

	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []string{"https://b.example.com"}, urlsOf(rows[0].Tabs))
}

func TestArchive_OwnWindowRemovedIsIgnored(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	require.NoError(t, h.svc.Archive(context.Background(), 1))

	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: w.ID})
	assert.Empty(t, h.clock.live())
	assert.Empty(t, h.svc.closing)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	h.clock.fire()

	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(rows[0].Tabs))
}

func TestArchive_DisconnectedMarksSnapshottedKeepingSavedTabs(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	_, err := h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)

	require.NoError(t, h.svc.Archive(context.Background(), 1))

	assert.Empty(t, h.driver.Closed())
	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(rows[0].Tabs))
}

func TestArchive_MissingRowsBecomeSnapshotted(t *testing.T) {
	h := newHarness(t)
	addMissing(t, h, 1, "https://a.example.com")

	require.NoError(t, h.svc.Archive(context.Background(), 1))

	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(rows[0].Tabs))
}

func TestArchive_WindowClosedByHandIsStillForgotten(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	removeWindow(h, res.WindowID)

	require.NoError(t, h.svc.Archive(context.Background(), 1))
	assert.Equal(t, 1, h.clock.fire())

	assert.Empty(t, rowsOf(t, h, 1))
}

func TestArchive_AttachedSessionIsNotRestoredByPlacement(t *testing.T) {
	h := newPlaceHarness(t)
	h.own(t, 1, "a", "2")
	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	require.NoError(t, h.svc.Archive(context.Background(), 1))
	require.NoError(t, h.svc.place(context.Background()))

	assert.Equal(t, uint64(0), h.svc.attached.Load())
	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, live)
	rows := rowsOf(t, h.harness, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
}

func TestDelete_ClosesWindowsAndDropsRows(t *testing.T) {
	h := newHarness(t)
	a := ownWindow(t, h, 1, "https://a.example.com")
	b := ownWindow(t, h, 1, "https://b.example.com")
	addMissing(t, h, 1, "https://c.example.com")
	other := ownWindow(t, h, 2, "https://other.example.com")

	require.NoError(t, h.svc.Delete(context.Background(), 1))
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: a.ID})
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: b.ID})

	assert.ElementsMatch(t, []string{a.ID, b.ID}, h.driver.Closed())
	assert.Empty(t, rowsOf(t, h, 1))
	assert.Empty(t, h.clock.live())
	assert.Empty(t, h.svc.closing)
	assert.Equal(t, uint(2), sessionOf(t, h, other.ID))
}

func TestDelete_DisconnectedDropsRowsWithoutClosing(t *testing.T) {
	h := newHarness(t)
	ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	require.NoError(t, h.svc.Delete(context.Background(), 1))

	assert.Empty(t, h.driver.Closed())
	assert.Empty(t, rowsOf(t, h, 1))
}

func TestPlace_AttachingArchivedSessionRestoresAndPlacesWindows(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "P") }
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	addSnapshotted(t, h.harness, 1, base,
		SavedTab{URL: "https://a.example.com"},
		SavedTab{URL: "https://b.example.com", Active: true},
		SavedTab{URL: "https://c.example.com"},
	)
	addSnapshotted(t, h.harness, 1, base.Add(time.Minute), SavedTab{URL: "https://d.example.com", Active: true})
	addSnapshotted(t, h.harness, 2, base, SavedTab{URL: "https://other.example.com"})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	byURL := map[string]string{}
	for _, r := range rows {
		w, ok := h.driver.Window(r.DriverWindowID)
		require.True(t, ok)
		byURL[w.Tabs[0].URL] = w.ID
		assert.Equal(t, "wm-"+w.ID, h.svc.links.links[w.ID])
		assert.Equal(t, "2", h.wm.Workspace("wm-"+w.ID))
	}
	abc, _ := h.driver.Window(byURL["https://a.example.com"])
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com", "https://c.example.com"}, tabURLs(abc))
	assert.Equal(t, "https://b.example.com", activeURL(t, h.harness, abc.ID))
	assert.Equal(t, byURL["https://d.example.com"], rows[0].DriverWindowID)
	assert.Empty(t, h.driver.Focused())
	assert.Empty(t, h.wm.Focused())
	restorable, err := h.store.ListRestorable(2)
	require.NoError(t, err)
	assert.Len(t, restorable, 1)
}

func TestPlace_SessionWithLiveWindowIsNotRestored(t *testing.T) {
	h := newPlaceHarness(t)
	h.own(t, 1, "a", "2")
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://b.example.com"})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Len(t, live, 1)
}

func TestPlace_RestoresWithoutWindowManager(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestRestore_EmptySnapshotOpensNewTab(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	w, ok := h.driver.Window(rows[0].DriverWindowID)
	require.True(t, ok)
	assert.Equal(t, []string{"chrome://newtab/"}, tabURLs(w))
}

func TestRestore_DisconnectedIsNoop(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	h.svc.mu.Lock()
	restored, err := h.svc.restoreLocked(context.Background(), 1)
	h.svc.mu.Unlock()
	require.NoError(t, err)
	assert.Empty(t, restored)

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Empty(t, live)
	restorable, err := h.store.ListRestorable(1)
	require.NoError(t, err)
	assert.Len(t, restorable, 1)
}

func TestRestore_SkipsMissingRowsWhileChromeIsRestoring(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 1, "https://a.example.com")
	connectRun(h, "r1")
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://b.example.com"})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, []string{"https://b.example.com"}, tabURLs(live[0]))
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Len(t, missing, 1)
}

func TestRestore_RestoresMissingRowsOutsideRestoreWindow(t *testing.T) {
	h := newHarness(t)
	addMissing(t, h, 1, "https://a.example.com")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	missing, err := h.store.ListMissing()
	require.NoError(t, err)
	assert.Empty(t, missing)
}

func TestRestore_RestoredWindowIsNotAdoptedByFocusedSession(t *testing.T) {
	h := newHarness(t)
	manualNow(h)
	addMissing(t, h, 5, "https://unrelated.example.com")
	connectRun(h, "r1")
	focusedParent(t, h, 3)
	addSnapshotted(t, h, 1, time.Time{})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	id := rows[0].DriverWindowID
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: id})
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: id})

	assert.Equal(t, uint(1), sessionOf(t, h, id))
	assert.Empty(t, h.svc.deferredAdopt)
	threeRows, err := h.store.ListOpenBySession(3)
	require.NoError(t, err)
	assert.Len(t, threeRows, 1)
}

func TestNewService_RestoreWindowDisabledByDefault(t *testing.T) {
	h := newHarness(t)
	h.svc.restoreWindow = NewService(h.driver, h.store, h.sessions, wm.None{}, Layout{}).restoreWindow
	connectRun(h, "run-a")
	addMissing(t, h, 1, "https://a.example.com/x")
	connectRun(h, "run-b")

	assert.False(t, h.svc.restoring())
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: "https://a.example.com/x"})
	require.NoError(t, err)
	assert.False(t, res.Created)
	assert.Equal(t, uint(1), sessionOf(t, h, res.WindowID))
}

func TestOpen_SnapshottedSessionRestoresAndOpensInRestoredWindow(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: prURL + "/files"}, SavedTab{URL: "https://a.example.com", Active: true})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	require.Len(t, live, 1)
	assert.Equal(t, live[0].ID, res.WindowID)
	assert.False(t, res.Created)
	assert.Equal(t, []string{prURL + "/files", "https://a.example.com"}, tabURLs(live[0]))
	assert.Equal(t, live[0].Tabs[0].ID, res.TabID)
	assert.Equal(t, prURL+"/files", activeURL(t, h, res.WindowID))
	assert.Equal(t, uint(1), sessionOf(t, h, res.WindowID))
}

func TestOpen_RestoredTargetIsMostRecentlyFocusedWindow(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	addSnapshotted(t, h, 1, base.Add(time.Minute), SavedTab{URL: "https://recent.example.com"})
	addSnapshotted(t, h, 1, base, SavedTab{URL: "https://old.example.com"})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	w, ok := h.driver.Window(res.WindowID)
	require.True(t, ok)
	assert.Equal(t, []string{"https://recent.example.com", prURL}, tabURLs(w))
	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Len(t, live, 2)
	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, res.WindowID, rows[0].DriverWindowID)
}

func TestOpen_AttachedSessionFocusesRestoredWindow(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "P") }
	h.sessions.AttachedID = 1
	h.svc.SetAttached(1)
	drain(h.svc)
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	assert.True(t, res.Focused)
	assert.Equal(t, []string{res.WindowID}, h.driver.Focused())
	assert.Equal(t, []string{"wm-" + res.WindowID}, h.wm.Focused())
	assert.Equal(t, "2", h.wm.Workspace("wm-"+res.WindowID))
}

func TestOpen_BackgroundSessionRestoresWithoutFocus(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "2") }
	h.sessions.AttachedID = 2
	h.svc.SetAttached(2)
	drain(h.svc)
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	assert.False(t, res.Focused)
	assert.Empty(t, h.driver.Focused())
	assert.Empty(t, h.wm.Focused())
	assert.Len(t, h.svc.swapCh, 1)
	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "P", h.wm.Workspace("wm-"+res.WindowID))
}

func TestModule_LifecycleEventsDoNotBlockPublisher(t *testing.T) {
	bus := eventbus.NewEventBus()
	driver := NewFakeDriver()
	m := NewModule(driver, &FakeSessions{}, wm.None{}, Layout{BrowserWorkspace: "2", ParkWorkspace: "P"}, bus, testdb.New(t, &BrowserWindow{}, &BrowserState{}))
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	archived := driver.AddWindow("https://a.example.com")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: archived.ID}))
	deleted := driver.AddWindow("https://b.example.com")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: deleted.ID}))

	m.Service.mu.Lock()
	done := make(chan struct{})
	go func() {
		_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.SessionArchived, Data: eventbus.SessionIDEvent{SessionID: 1}})
		_ = bus.Publish(context.Background(), eventbus.Event{Type: eventbus.SessionDeleted, Data: eventbus.SessionIDEvent{SessionID: 2}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publish blocked on the service lock")
	}
	m.Service.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go m.Service.lifecycleLoop(ctx)

	assert.Eventually(t, func() bool { return len(driver.Closed()) == 2 }, time.Second, 10*time.Millisecond)
	assert.Eventually(t, func() bool {
		m.Service.mu.Lock()
		defer m.Service.mu.Unlock()
		one, err := m.Service.store.ListBySession(1)
		require.NoError(t, err)
		two, err := m.Service.store.ListBySession(2)
		require.NoError(t, err)
		return len(one) == 1 && one[0].State == WindowSnapshotted && len(two) == 0
	}, time.Second, 10*time.Millisecond)
}

func failCreateFor(h harness, url string) {
	h.driver.CreateErr = func(urls []string) error {
		if len(urls) > 0 && urls[0] == url {
			return errors.New("create broke")
		}
		return nil
	}
}

func TestRestore_FailedRowDoesNotStopLaterRows(t *testing.T) {
	h := newHarness(t)
	base := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	addSnapshotted(t, h, 1, base.Add(time.Minute), SavedTab{URL: "https://bad.example.com"})
	addSnapshotted(t, h, 1, base, SavedTab{URL: "https://good.example.com"})
	failCreateFor(h, "https://bad.example.com")

	h.svc.mu.Lock()
	restored, err := h.svc.restoreLocked(context.Background(), 1)
	h.svc.mu.Unlock()

	assert.Error(t, err)
	require.Len(t, restored, 1)
	assert.Equal(t, []string{"https://good.example.com"}, tabURLs(restored[0]))
	restorable, err := h.store.ListRestorable(1)
	require.NoError(t, err)
	require.Len(t, restorable, 1)
	assert.Equal(t, []string{"https://bad.example.com"}, urlsOf(restorable[0].Tabs))
}

func TestOpen_FailedRestoreFallsBackToCreate(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://bad.example.com"})
	failCreateFor(h, "https://bad.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	assert.True(t, res.Created)
	w, ok := h.driver.Window(res.WindowID)
	require.True(t, ok)
	assert.Equal(t, []string{prURL}, tabURLs(w))
	assert.Equal(t, uint(1), sessionOf(t, h, res.WindowID))
}

func TestOpen_RestoreUnavailableIsReturned(t *testing.T) {
	h := newHarness(t)
	addSnapshotted(t, h, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	h.driver.CreateErr = func([]string) error { return ErrNotConnected }

	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})

	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestSavedTabs_KeepsOnlyWebPages(t *testing.T) {
	w := Window{Tabs: []Tab{
		{URL: "https://a.example.com"},
		{URL: "http://b.example.com"},
		{URL: "chrome://settings"},
		{URL: "file:///tmp/x.html"},
		{URL: "chrome-extension://abc/page.html"},
		{URL: "devtools://devtools/bundled/inspector.html"},
		{URL: "HTTPS://C.example.com"},
	}}

	assert.Equal(t, []string{"https://a.example.com", "http://b.example.com", "HTTPS://C.example.com"}, urlsOf(savedTabs(w)))
}

func TestArchive_DropsDeferredAdoptionsIntoSession(t *testing.T) {
	h := newHarness(t)
	h.svc.deferredAdopt["w-a"] = 1
	h.svc.deferredAdopt["w-b"] = 2

	require.NoError(t, h.svc.Archive(context.Background(), 1))

	assert.Equal(t, map[string]uint{"w-b": 2}, h.svc.deferredAdopt)
}

func TestDelete_DropsDeferredAdoptionsIntoSession(t *testing.T) {
	h := newHarness(t)
	h.svc.deferredAdopt["w-a"] = 1
	h.svc.deferredAdopt["w-b"] = 2

	require.NoError(t, h.svc.Delete(context.Background(), 1))

	assert.Equal(t, map[string]uint{"w-b": 2}, h.svc.deferredAdopt)
}

func TestArchive_CloseFailureKeepsWindowOwned(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.driver.CloseErr = errors.New("close broke")

	assert.Error(t, h.svc.Archive(context.Background(), 1))

	got, err := h.store.GetByDriverWindowID(w.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(got.Tabs))
	assert.Empty(t, h.svc.closing)
}

func TestRestore_SharesOneLinkDeadline(t *testing.T) {
	h := newPlaceHarness(t)
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://b.example.com"})
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://c.example.com"})

	h.svc.SetAttached(1)
	start := time.Now()
	require.NoError(t, h.svc.place(context.Background()))
	elapsed := time.Since(start)

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 3)
	assert.GreaterOrEqual(t, elapsed, createdLinkWait)
	assert.Less(t, elapsed, 2*createdLinkWait)
}

func TestArchive_DisconnectedClosesWindowOnSameRunReconnect(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	w := ownWindow(t, h, 1, "https://a.example.com")
	connectRun(h, "r1")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	_, err := h.driver.OpenTab(context.Background(), w.ID, "https://b.example.com")
	require.NoError(t, err)

	require.NoError(t, h.svc.Archive(context.Background(), 1))
	assert.Empty(t, h.driver.Closed())
	connectRun(h, "r1")

	assert.Equal(t, []string{w.ID}, h.driver.Closed())
	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowSnapshotted, rows[0].State)
	assert.Equal(t, []string{"https://a.example.com", "https://b.example.com"}, urlsOf(rows[0].Tabs))
	assert.Empty(t, h.svc.pendingClose)
	assert.Equal(t, uint(0), sessionOf(t, h, w.ID))
}

func TestArchive_DisconnectedReconnectNeverSavesEmptyTabs(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	w := ownWindow(t, h, 1, "https://a.example.com")
	connectRun(h, "r1")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	h.driver.RemoveWindow(w.ID)
	empty := h.driver.AddWindow("chrome://newtab/")
	h.svc.pendingClose[empty.ID] = pendingCloseOp{rowID: rowsOf(t, h, 1)[0].ID}

	require.NoError(t, h.svc.Archive(context.Background(), 1))
	connectRun(h, "r1")

	assert.Equal(t, []string{empty.ID}, h.driver.Closed())
	assert.Equal(t, []string{"https://a.example.com"}, urlsOf(rowsOf(t, h, 1)[0].Tabs))
}

func TestDelete_DisconnectedClosesWindowOnSameRunReconnect(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	require.NoError(t, h.svc.Delete(context.Background(), 1))
	assert.Empty(t, h.driver.Closed())
	connectRun(h, "r1")

	assert.Equal(t, []string{w.ID}, h.driver.Closed())
	assert.Empty(t, rowsOf(t, h, 1))
	assert.Empty(t, h.svc.pendingClose)
}

func TestPendingClose_ClearedOnRunChange(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	require.NoError(t, h.svc.Delete(context.Background(), 1))
	require.Contains(t, h.svc.pendingClose, w.ID)

	connectRun(h, "r2")

	assert.Empty(t, h.driver.Closed())
	assert.Empty(t, h.svc.pendingClose)
}

func TestLifecycle_QueuedArchiveSkippedWhenSessionAttachedAgain(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.sessions.AttachedID = 1

	require.NoError(t, h.svc.runLifecycle(context.Background(), lifecycleOp{sessionID: 1}))

	assert.Empty(t, h.driver.Closed())
	assert.Equal(t, uint(1), sessionOf(t, h, w.ID))
}

func TestLifecycle_QueuedArchiveRunsForDetachedSession(t *testing.T) {
	h := newHarness(t)
	w := ownWindow(t, h, 1, "https://a.example.com")
	h.sessions.AttachedID = 2

	require.NoError(t, h.svc.runLifecycle(context.Background(), lifecycleOp{sessionID: 1}))

	assert.Equal(t, []string{w.ID}, h.driver.Closed())
}

func TestOpen_RestoredWindowIsPlacedBeforeFocus(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "P") }
	h.sessions.AttachedID = 1
	h.svc.SetAttached(1)
	drain(h.svc)
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	workspaceAtFocus := ""
	h.driver.OnFocus = func(id string) { workspaceAtFocus = h.wm.Workspace("wm-" + id) }

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	assert.True(t, res.Focused)
	assert.Equal(t, "2", workspaceAtFocus)
}

func TestConnected_RestoreWindowEndSignalsPlacement(t *testing.T) {
	h := newHarness(t)
	addMissing(t, h, 2, "https://a.example.com")
	connectRun(h, "r1")
	drain(h.svc)

	var end *fakeTimer
	for _, tm := range h.clock.live() {
		if tm.d == testRestoreWindow {
			end = tm
		}
	}
	require.NotNil(t, end)
	end.f()

	assert.Len(t, h.svc.swapCh, 1)
}
