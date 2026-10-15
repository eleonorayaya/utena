package browser

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type placeHarness struct {
	harness
	wm *wm.FakeDriver
}

func newPlaceHarness(t *testing.T) placeHarness {
	t.Helper()
	store := NewStore(testdb.New(t, &BrowserWindow{}, &BrowserState{}))
	driver := NewFakeDriver()
	sessions := &FakeSessions{}
	wmd := wm.NewFakeDriver()
	svc := NewService(driver, store, sessions, wmd, Layout{BrowserWorkspace: "2", ParkWorkspace: "P"})
	svc.handleEvent(context.Background(), Event{Type: EventConnected})
	drain(svc)
	return placeHarness{harness: harness{svc: svc, driver: driver, sessions: sessions, store: store}, wm: wmd}
}

func drain(s *Service) {
	select {
	case <-s.swapCh:
	default:
	}
}

func (h placeHarness) own(t *testing.T, sessionID uint, title, workspace string) string {
	t.Helper()
	w := h.driver.AddWindow("https://" + title + ".example.com")
	h.driver.SetTitle(w.ID, title)
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: sessionID, State: WindowOpen, DriverWindowID: w.ID}))
	wmID := "wm-" + w.ID
	h.wm.Add(wmID, title+" - Fake", workspace)
	return wmID
}

func TestPlace_SwapsAttachedSessionWindows(t *testing.T) {
	h := newPlaceHarness(t)
	a := h.own(t, 1, "a", "2")
	b := h.own(t, 2, "b", "2")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "2", h.wm.Workspace(a))
	assert.Equal(t, "P", h.wm.Workspace(b))

	h.svc.SetAttached(2)
	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "P", h.wm.Workspace(a))
	assert.Equal(t, "2", h.wm.Workspace(b))
}

func TestPlace_NoAttachedSessionParksEverything(t *testing.T) {
	h := newPlaceHarness(t)
	a := h.own(t, 1, "a", "2")

	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "P", h.wm.Workspace(a))
}

func TestPlace_NeverMovesUnownedWindows(t *testing.T) {
	h := newPlaceHarness(t)
	personal := h.driver.AddWindow("https://personal.example.com")
	h.driver.SetTitle(personal.ID, "personal")
	h.wm.Add("wm-personal", "personal - Fake", "2")
	b := h.own(t, 2, "b", "2")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Equal(t, "2", h.wm.Workspace("wm-personal"))
	assert.Equal(t, []wm.Move{{WindowID: b, Workspace: "P"}}, h.wm.Moves())
}

func TestPlace_UntitledOwnedWindowIsNotEliminatedAgainstUnownedWindow(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.AddWindow("https://personal.example.com")
	owned := h.driver.AddWindow("https://owned.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: owned.ID}))
	h.wm.Add("wm-personal", "personal - Fake", "2")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Empty(t, h.wm.Moves())
}

func TestPlace_WindowAlreadyInPlaceDoesNotMove(t *testing.T) {
	h := newPlaceHarness(t)
	h.own(t, 1, "a", "2")
	h.own(t, 2, "b", "P")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Empty(t, h.wm.Moves())
}

func TestPlace_AmbiguousTitlesMoveNothing(t *testing.T) {
	h := newPlaceHarness(t)
	h.own(t, 2, "same", "2")
	h.own(t, 2, "same", "2")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Empty(t, h.wm.Moves())
}

func TestPlace_MoveErrorIsLoggedNotReturned(t *testing.T) {
	h := newPlaceHarness(t)
	a := h.own(t, 2, "a", "2")
	h.wm.MoveErr = errors.New("aerospace broke")

	h.svc.SetAttached(1)
	assert.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "2", h.wm.Workspace(a))
}

func TestPlace_SkippedWhenDisconnected(t *testing.T) {
	h := newPlaceHarness(t)
	h.own(t, 2, "a", "2")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Empty(t, h.wm.Moves())
}

func TestOpen_BackgroundWindowIsLinkedByCreationAndParked(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.AddWindow("https://personal.example.com")
	h.wm.Add("wm-personal", "", "2")
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "2") }
	h.svc.SetAttached(2)
	drain(h.svc)

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	require.True(t, res.Created)
	assert.Len(t, h.svc.swapCh, 1)

	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, []wm.Move{{WindowID: "wm-" + res.WindowID, Workspace: "P"}}, h.wm.Moves())
}

func TestOpen_WithNoWindowManagerDoesNotPoll(t *testing.T) {
	h := newHarness(t)
	h.sessions.AttachedID = 2

	start := time.Now()
	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
	assert.Less(t, time.Since(start), 500*time.Millisecond)

	h.svc.SetAttached(2)
	assert.NoError(t, h.svc.place(context.Background()))
}

func TestOpen_ActivatingSetsAttachedBeforeSwap(t *testing.T) {
	h := newPlaceHarness(t)
	h.sessions.AttachedID = 2
	h.svc.SetAttached(2)
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "P") }

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 1, URL: prURL, Activate: true})
	require.NoError(t, err)
	assert.Equal(t, uint64(1), h.svc.attached.Load())

	require.NoError(t, h.svc.place(context.Background()))
	assert.Equal(t, "2", h.wm.Workspace("wm-"+res.WindowID))
}

func TestSetAttached_CoalescesAndDoesNotWaitForServiceLock(t *testing.T) {
	h := newPlaceHarness(t)
	h.svc.mu.Lock()
	defer h.svc.mu.Unlock()

	done := make(chan struct{})
	go func() {
		h.svc.SetAttached(1)
		h.svc.SetAttached(2)
		h.svc.SetAttached(3)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SetAttached blocked")
	}
	assert.Len(t, h.svc.swapCh, 1)
	assert.Equal(t, uint64(3), h.svc.attached.Load())
}

func TestPlaceLoop_PlacesAfterSignalAndExitsOnCancel(t *testing.T) {
	h := newPlaceHarness(t)
	b := h.own(t, 2, "b", "2")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	done := make(chan struct{})
	go func() {
		h.svc.placeLoop(ctx)
		close(done)
	}()

	h.svc.SetAttached(1)
	assert.Eventually(t, func() bool { return h.wm.Workspace(b) == "P" }, time.Second, 10*time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("placeLoop did not exit")
	}
}

func TestResync_SeedsAttachedFromSessions(t *testing.T) {
	h := newPlaceHarness(t)
	h.sessions.AttachedID = 4
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	assert.Equal(t, uint64(4), h.svc.attached.Load())

	h.svc.SetAttached(5)
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	assert.Equal(t, uint64(5), h.svc.attached.Load())
}

func TestConnected_SignalsPlacement(t *testing.T) {
	h := newPlaceHarness(t)
	h.svc.handleEvent(context.Background(), Event{Type: EventConnected})
	assert.Len(t, h.svc.swapCh, 1)
}

func TestAdopt_SignalsPlacementOnlyWhenRowAdded(t *testing.T) {
	h := newPlaceHarness(t)
	parent := h.driver.AddWindow("https://a.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: parent.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventFocusChanged, WindowID: parent.ID})

	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: parent.ID})
	assert.Empty(t, h.svc.swapCh)

	child := h.driver.AddWindow("chrome://newtab")
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowCreated, WindowID: child.ID})
	assert.Len(t, h.svc.swapCh, 1)
}

func TestTabsChanged_SignalsOnlyForOwnedUnlinkedWindows(t *testing.T) {
	h := newPlaceHarness(t)
	personal := h.driver.AddWindow("https://personal.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: personal.ID})
	assert.Empty(t, h.svc.swapCh)

	owned := h.driver.AddWindow("https://owned.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: owned.ID}))
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: owned.ID})
	assert.Len(t, h.svc.swapCh, 1)
	drain(h.svc)

	h.driver.SetTitle(owned.ID, "owned")
	h.wm.Add("wm-owned", "owned - Fake", "2")
	require.NoError(t, h.svc.place(context.Background()))
	h.svc.handleEvent(context.Background(), Event{Type: EventTabsChanged, WindowID: owned.ID})
	assert.Empty(t, h.svc.swapCh)
}

func TestModule_SessionAttachedEventSetsAttached(t *testing.T) {
	bus := eventbus.NewEventBus()
	m := NewModule(NewFakeDriver(), &FakeSessions{}, wm.NewFakeDriver(), Layout{BrowserWorkspace: "2", ParkWorkspace: "P"}, bus, testdb.New(t, &BrowserWindow{}, &BrowserState{}))

	require.NoError(t, bus.Publish(context.Background(), eventbus.Event{Type: eventbus.SessionAttached, Data: eventbus.SessionAttachedEvent{SessionID: 7}}))

	assert.Equal(t, uint64(7), m.Service.attached.Load())
	assert.Len(t, m.Service.swapCh, 1)
}

func TestFakeSessions_Attached(t *testing.T) {
	f := &FakeSessions{AttachedID: 3}
	id, err := f.Attached(context.Background())
	require.NoError(t, err)
	assert.Equal(t, uint(3), id)
}
