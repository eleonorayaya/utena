package browser

import (
	"context"
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type harness struct {
	svc      *Service
	driver   *FakeDriver
	sessions *FakeSessions
	store    *Store
}

func newHarness(t *testing.T) harness {
	t.Helper()
	store := NewStore(testdb.New(t, &BrowserWindow{}))
	driver := NewFakeDriver()
	sessions := &FakeSessions{}
	svc := NewService(driver, store, sessions)
	svc.handleEvent(context.Background(), Event{Type: EventConnected})
	return harness{svc: svc, driver: driver, sessions: sessions, store: store}
}

const prURL = "https://github.com/o/r/pull/1"

func TestOpen_UnavailableWhenDisconnected(t *testing.T) {
	h := newHarness(t)
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestOpen_BackgroundSessionCreatesWindowQuietly(t *testing.T) {
	h := newHarness(t)
	h.sessions.Attached = 2

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
	assert.False(t, res.Focused)
	assert.Empty(t, h.driver.Focused())

	rows, err := h.store.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, res.WindowID, rows[0].DriverWindowID)
}

func TestOpen_AttachedSessionFocuses(t *testing.T) {
	h := newHarness(t)
	h.sessions.Attached = 1

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Focused)
	assert.Contains(t, h.driver.Focused(), res.WindowID)
}

func TestOpen_ReusesWindowAndOpensNewTab(t *testing.T) {
	h := newHarness(t)
	first, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)

	second, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: "https://docs.example.com/x"})
	require.NoError(t, err)
	assert.False(t, second.Created)
	assert.Equal(t, first.WindowID, second.WindowID)

	w, _ := h.driver.Window(first.WindowID)
	assert.Len(t, w.Tabs, 2)
}

func TestOpen_DedupActivatesExistingTab(t *testing.T) {
	h := newHarness(t)
	first, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL + "/files"})
	require.NoError(t, err)
	_, err = h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: "https://other.example.com"})
	require.NoError(t, err)

	again, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, first.TabID, again.TabID)

	w, _ := h.driver.Window(first.WindowID)
	assert.Len(t, w.Tabs, 2)
	for _, tab := range w.Tabs {
		assert.Equal(t, tab.ID == first.TabID, tab.Active)
	}
}

func TestOpen_PrefersMostRecentlyFocusedWindow(t *testing.T) {
	h := newHarness(t)
	a := h.driver.AddWindow("https://a.example.com")
	b := h.driver.AddWindow("https://b.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: a.ID}))
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: b.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: a.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, a.ID, res.WindowID)
}

func TestOpen_SkipsRowsWhoseWindowIsGone(t *testing.T) {
	h := newHarness(t)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "ghost"}))

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
}

func TestOpen_ActivateSwitchesThenFocuses(t *testing.T) {
	h := newHarness(t)
	h.sessions.Attached = 2

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL, Activate: true})
	require.NoError(t, err)
	assert.Equal(t, []uint{1}, h.sessions.Activated)
	assert.True(t, res.Focused)
}

func TestOpen_ActivateSkippedWhenAlreadyAttached(t *testing.T) {
	h := newHarness(t)
	h.sessions.Attached = 1
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL, Activate: true})
	require.NoError(t, err)
	assert.Empty(t, h.sessions.Activated)
}

func TestOpen_ActivateErrorAborts(t *testing.T) {
	h := newHarness(t)
	h.sessions.ActivateErr = errors.New("broken")
	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL, Activate: true})
	assert.Error(t, err)
	windows, _ := h.driver.ListWindows(context.Background())
	assert.Empty(t, windows)
}

func TestOpen_NoSessionUsesUnownedWindowAndFocuses(t *testing.T) {
	h := newHarness(t)
	owned := h.driver.AddWindow("https://owned.example.com")
	personal := h.driver.AddWindow("https://personal.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: owned.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: owned.ID})

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, personal.ID, res.WindowID)
	assert.True(t, res.Focused)
}

func TestOpen_NoSessionCreatesUnownedWindowWhenAllOwned(t *testing.T) {
	h := newHarness(t)
	owned := h.driver.AddWindow("https://owned.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: owned.ID}))

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
	_, err = h.store.GetByDriverWindowID(res.WindowID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestOpen_DriverNotConnectedIsUnavailable(t *testing.T) {
	h := newHarness(t)
	h.driver.ListErr = ErrNotConnected

	_, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	assert.ErrorIs(t, err, ErrUnavailable)
}
