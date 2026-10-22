package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func windowIDOf(wmID string) string {
	return strings.TrimPrefix(wmID, "wm-")
}

func TestEject_ForgetsWindowAndLeavesItOpen(t *testing.T) {
	h := newPlaceHarness(t)
	h.sessions.AttachedID = 1
	wmID := h.own(t, 1, "a", "2")
	windowID := windowIDOf(wmID)

	res, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowID})
	require.NoError(t, err)
	assert.Equal(t, EjectResult{WindowID: windowID}, res)

	_, err = h.store.GetByDriverWindowID(windowID)
	assert.ErrorIs(t, err, ErrWindowNotFound)
	_, ok := h.driver.Window(windowID)
	assert.True(t, ok)
	assert.Empty(t, h.driver.Closed())
	assert.Empty(t, h.wm.Moves())
}

func TestEject_MovesParkedWindowToBrowserWorkspace(t *testing.T) {
	h := newPlaceHarness(t)
	h.sessions.AttachedID = 2
	wmID := h.own(t, 1, "a", "P")

	res, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowIDOf(wmID)})
	require.NoError(t, err)
	assert.True(t, res.Moved)
	assert.Equal(t, []wm.Move{{WindowID: wmID, Workspace: "2"}}, h.wm.Moves())
}

func TestEject_PlacementLeavesEjectedWindowAlone(t *testing.T) {
	h := newPlaceHarness(t)
	wmID := h.own(t, 1, "a", "P")
	_, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowIDOf(wmID)})
	require.NoError(t, err)

	h.svc.SetAttached(2)
	require.NoError(t, h.svc.place(context.Background()))

	assert.Equal(t, "2", h.wm.Workspace(wmID))
}

func TestEject_EjectedWindowTakesSessionlessLinks(t *testing.T) {
	h := newPlaceHarness(t)
	wmID := h.own(t, 1, "a", "2")
	windowID := windowIDOf(wmID)
	_, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowID})
	require.NoError(t, err)

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL})
	require.NoError(t, err)
	assert.Equal(t, windowID, res.WindowID)
}

func TestEject_RejectsWindowOfAnotherSession(t *testing.T) {
	h := newPlaceHarness(t)
	wmID := h.own(t, 2, "a", "P")
	windowID := windowIDOf(wmID)

	_, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowID})
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, common.CategoryInvalidRequest, appErr.Category)

	_, err = h.store.GetByDriverWindowID(windowID)
	assert.NoError(t, err)
	assert.Empty(t, h.wm.Moves())
}

func TestEject_RejectsWindowThatIsNotLive(t *testing.T) {
	h := newPlaceHarness(t)
	wmID := h.own(t, 1, "a", "P")
	windowID := windowIDOf(wmID)
	h.driver.RemoveWindow(windowID)

	_, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowID})
	assert.ErrorIs(t, err, ErrWindowNotFound)
	_, err = h.store.GetByDriverWindowID(windowID)
	assert.NoError(t, err)
}

func TestEject_UnavailableWhenDisconnected(t *testing.T) {
	h := newPlaceHarness(t)
	wmID := h.own(t, 1, "a", "P")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	_, err := h.svc.Eject(context.Background(), EjectRequest{SessionID: 1, WindowID: windowIDOf(wmID)})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestOpen_AttachedLinkGoesToAttachedSessionWindow(t *testing.T) {
	h := newHarness(t)
	h.driver.AddWindow("https://personal.example.com")
	session := h.driver.AddWindow("https://session.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: session.ID}))
	h.sessions.AttachedID = 1

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL, Attached: true})
	require.NoError(t, err)
	assert.Equal(t, session.ID, res.WindowID)
	assert.True(t, res.Focused)
}

func TestOpen_AttachedLinkWithoutAttachedSessionUsesUnownedWindow(t *testing.T) {
	h := newHarness(t)
	personal := h.driver.AddWindow("https://personal.example.com")

	res, err := h.svc.Open(context.Background(), OpenRequest{URL: prURL, Attached: true})
	require.NoError(t, err)
	assert.Equal(t, personal.ID, res.WindowID)
}

func TestOpen_ExplicitSessionWinsOverAttached(t *testing.T) {
	h := newHarness(t)
	other := h.driver.AddWindow("https://other.example.com")
	require.NoError(t, h.store.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: other.ID}))
	h.sessions.AttachedID = 1

	res, err := h.svc.Open(context.Background(), OpenRequest{SessionID: 2, URL: prURL, Attached: true})
	require.NoError(t, err)
	assert.Equal(t, other.ID, res.WindowID)
}
