package browser

import (
	"context"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/wm"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResync_MarksMissingWindowsAndKeepsLiveOnes(t *testing.T) {
	h := newHarness(t)
	live := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: live.ID}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "dead"}))

	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, live.ID, rows[0].DriverWindowID)
}

func TestWindowCreated_InheritsOwnerFromFocusedWindow(t *testing.T) {
	h := newHarness(t)
	parent := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: parent.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})

	child := h.driver.AddWindow("chrome://newtab")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: child.ID})

	got, err := h.store.GetByDriverWindowID(child.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(3), got.SessionID)
}

func TestWindowCreated_InheritsWhenFocusEventArrivesFirst(t *testing.T) {
	h := newHarness(t)
	parent := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: parent.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})

	child := h.driver.AddWindow("chrome://newtab")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: child.ID})
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: child.ID})

	got, err := h.store.GetByDriverWindowID(child.ID)
	require.NoError(t, err)
	assert.Equal(t, uint(3), got.SessionID)
}

func TestWindowCreated_FromUnownedStaysUnowned(t *testing.T) {
	h := newHarness(t)
	parent := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})

	child := h.driver.AddWindow("chrome://newtab")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: child.ID})

	_, err := h.store.GetByDriverWindowID(child.ID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestWindowCreated_IgnoresWindowsUtenaCreated(t *testing.T) {
	h := newHarness(t)
	other := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 9, State: WindowOpen, DriverWindowID: other.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: other.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: res.WindowID})

	got, err := h.store.GetByDriverWindowID(res.WindowID)
	require.NoError(t, err)
	assert.Equal(t, uint(1), got.SessionID)
}

func TestWindowRemoved_DeletesRow(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: res.WindowID})

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestFollowEvents_DeliversDriverEvents(t *testing.T) {
	h := newHarness(t)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.svc.followEvents(ctx)

	require.Eventually(t, func() bool { return h.driver.Emit(Event{Type: EventConnected}) }, time.Second, 10*time.Millisecond)
	assert.Eventually(t, h.svc.Connected, time.Second, 10*time.Millisecond)
}

func TestWindowCreated_DaemonCreatedUnownedWindowStaysUnowned(t *testing.T) {
	h := newHarness(t)
	owned := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: owned.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: owned.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	require.True(t, res.Created)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: res.WindowID})

	_, err = h.store.GetByDriverWindowID(res.WindowID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestWindowCreated_DaemonCreatedUnownedWindowStaysUnownedWhenFocusArrivesFirst(t *testing.T) {
	h := newHarness(t)
	owned := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: owned.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: owned.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	require.True(t, res.Created)
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: res.WindowID})
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: res.WindowID})

	_, err = h.store.GetByDriverWindowID(res.WindowID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestResync_TakesFocusFromDriver(t *testing.T) {
	h := newHarness(t)
	a := h.driver.AddWindow("https://a.example.com")
	b := h.driver.AddWindow("https://b.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: a.ID})

	h.driver.SetFocused(b.ID)
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, b.ID, res.WindowID)
}

func TestResync_ClearsStaleFocusWhenNoWindowFocused(t *testing.T) {
	h := newHarness(t)
	b := h.driver.AddWindow("https://b.example.com")
	a := h.driver.AddWindow("https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: a.ID})

	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, b.ID, res.WindowID)
}

func TestOpen_NoSessionPrefersFocusedUnownedWindow(t *testing.T) {
	h := newHarness(t)
	h.driver.AddWindow("https://first.example.com")
	second := h.driver.AddWindow("https://second.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: second.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, second.ID, res.WindowID)
}

func connectRun(h harness, run string) {
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected, Run: run})
}

func TestConnected_SameRunKeepsLiveRows(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "run-a")
	w := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID}))

	connectRun(h, "run-a")

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, w.ID, rows[0].DriverWindowID)
}

func TestConnected_NewRunForgetsAllWindowsEvenWhenIDsCollide(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "run-a")
	w := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID}))

	connectRun(h, "run-b")

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestConnected_EmptyRunKeepsLiveRows(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "run-a")
	w := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID}))

	connectRun(h, "")

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestConnected_RunPersistsAcrossServices(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "run-a")
	w := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: w.ID}))

	restarted := NewService(h.driver, h.store, h.sessions, wm.None{}, Layout{BrowserWorkspace: "2", ParkWorkspace: "P"})
	restarted.handleEvent(context.Background(), Event{Type: EventConnected, Run: "run-a"})

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Len(t, rows, 1)

	again := NewService(h.driver, h.store, h.sessions, wm.None{}, Layout{BrowserWorkspace: "2", ParkWorkspace: "P"})
	again.handleEvent(context.Background(), Event{Type: EventConnected, Run: "run-b"})

	rows, err = h.store.ListOpenBySession(1)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
