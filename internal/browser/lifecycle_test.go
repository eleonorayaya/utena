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
