package browser

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func liveWindows(t *testing.T, h harness) []Window {
	t.Helper()
	live, err := h.driver.ListWindows(context.Background())
	require.NoError(t, err)
	return live
}

func TestPlace_AttachedSessionWithoutWindowGetsOne(t *testing.T) {
	h := newHarness(t)

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	live := liveWindows(t, h)
	require.Len(t, live, 1)
	assert.Empty(t, live[0].Tabs)
	rows := rowsOf(t, h, 1)
	require.Len(t, rows, 1)
	assert.Equal(t, WindowOpen, rows[0].State)
	assert.Equal(t, live[0].ID, rows[0].DriverWindowID)
	assert.Empty(t, h.driver.Focused())
}

func TestPlace_AttachedSessionWithWindowGetsNoOther(t *testing.T) {
	h := newHarness(t)
	ownWindow(t, h, 1, "https://a.example.com")

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Len(t, liveWindows(t, h), 1)
}

func TestPlace_WindowClosedByHandComesBackOnlyOnNextAttach(t *testing.T) {
	h := newHarness(t)
	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	w := liveWindows(t, h)[0]

	h.driver.RemoveWindow(w.ID)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: w.ID})
	h.clock.fire()
	require.Empty(t, rowsOf(t, h, 1))

	require.NoError(t, h.svc.place(context.Background()))
	assert.Empty(t, liveWindows(t, h))

	h.svc.SetAttached(2)
	require.NoError(t, h.svc.place(context.Background()))
	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Len(t, rowsOf(t, h, 1), 1)
	assert.Len(t, liveWindows(t, h), 2)
}

func TestPlace_BrowserRestartProvidesWindowAgainButReconnectDoesNot(t *testing.T) {
	h := newHarness(t)
	connectRun(h, "r1")
	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))
	w := liveWindows(t, h)[0]
	h.driver.RemoveWindow(w.ID)
	h.svc.handleEvent(context.Background(), Event{Type: EventWindowRemoved, WindowID: w.ID})
	h.clock.fire()

	connectRun(h, "r1")
	require.NoError(t, h.svc.place(context.Background()))
	assert.Empty(t, liveWindows(t, h))

	connectRun(h, "r2")
	require.NoError(t, h.svc.place(context.Background()))
	assert.Len(t, liveWindows(t, h), 1)
}

func TestPlace_SessionWindowThatTakesFocusGivesItBack(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.Caps = Capabilities{CreateTakesFocus: true}
	h.svc.focusSettle = 0
	h.svc.focusPoll = time.Millisecond
	h.wm.SetCurrent("term")
	h.driver.OnCreate = func(w Window) {
		h.wm.Add("wm-"+w.ID, "", "2")
		go func() {
			time.Sleep(20 * time.Millisecond)
			h.wm.SetCurrent("wm-" + w.ID)
		}()
	}

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	cur, ok, err := h.wm.FocusedWindow(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "term", cur.ID)
	assert.Equal(t, []string{"term"}, h.wm.Focused())
}

func TestPlace_SessionWindowWithoutFocusCapabilityLeavesFocusAlone(t *testing.T) {
	h := newPlaceHarness(t)
	h.wm.SetCurrent("term")
	h.driver.OnCreate = func(w Window) { h.wm.Add("wm-"+w.ID, "", "2") }

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Empty(t, h.wm.Focused())
}

func TestPlace_RestoreThatTakesFocusGivesItBack(t *testing.T) {
	h := newPlaceHarness(t)
	h.driver.Caps = Capabilities{CreateTakesFocus: true, LoadTakesFocus: true}
	h.svc.focusSettle = 0
	h.svc.focusPoll = time.Millisecond
	addSnapshotted(t, h.harness, 1, time.Time{}, SavedTab{URL: "https://a.example.com"})
	h.wm.SetCurrent("term")
	h.driver.OnCreate = func(w Window) {
		h.wm.Add("wm-"+w.ID, "", "2")
		h.wm.SetCurrent("wm-" + w.ID)
	}

	h.svc.SetAttached(1)
	require.NoError(t, h.svc.place(context.Background()))

	cur, _, err := h.wm.FocusedWindow(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "term", cur.ID)
}
