package browser

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireCategory(t *testing.T, err error, category common.ErrorCategory) {
	t.Helper()
	var appErr *common.AppError
	require.ErrorAs(t, err, &appErr)
	assert.Equal(t, category, appErr.Category)
}

func TestMoveTab_MovesIntoTargetSessionWindowAndActivatesIt(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com", "https://b.example.com")
	dst := ownWindow(t, h, 2, "https://c.example.com")

	res, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 2})
	require.NoError(t, err)
	assert.Equal(t, MoveTabResult{WindowID: dst.ID}, res)

	gotSrc, _ := h.driver.Window(src.ID)
	assert.Equal(t, []string{"https://b.example.com"}, tabURLs(gotSrc))
	gotDst, _ := h.driver.Window(dst.ID)
	assert.Equal(t, []string{"https://c.example.com", "https://a.example.com"}, tabURLs(gotDst))
	active, _ := gotDst.ActiveTab()
	assert.Equal(t, src.Tabs[0].ID, active.ID)
	assert.Empty(t, h.driver.Focused())
}

func TestMoveTab_CreatesWindowForSessionWithoutOne(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com", "https://b.example.com")

	res, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[1].ID, ToSessionID: 2})
	require.NoError(t, err)
	assert.True(t, res.Created)
	assert.NotEqual(t, src.ID, res.WindowID)

	row, err := h.store.GetByDriverWindowID(res.WindowID)
	require.NoError(t, err)
	assert.Equal(t, uint(2), row.SessionID)
	assert.Equal(t, []string{"https://b.example.com"}, savedURLs(row.Tabs))
	assert.Empty(t, h.driver.Focused())
}

func TestMoveTab_NewWindowIsPlacedForTargetSession(t *testing.T) {
	h := newPlaceHarness(t)
	h.sessions.AttachedID = 1
	h.svc.SetAttached(1)
	drain(h.svc)
	srcWM := h.own(t, 1, "a", "2")
	src, _ := h.driver.Window(windowIDOf(srcWM))
	h.driver.AddWindow()

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 2})
	require.NoError(t, err)

	select {
	case <-h.svc.swapCh:
	default:
		t.Fatal("placement was not signalled for the new window")
	}
}

func TestMoveTab_RejectsWindowOfAnotherSession(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 3, "https://a.example.com")

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 2})
	requireCategory(t, err, common.CategoryInvalidRequest)
	got, _ := h.driver.Window(src.ID)
	assert.Len(t, got.Tabs, 1)
}

func TestMoveTab_RejectsTabNotInWindow(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com")

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: "nope", ToSessionID: 2})
	requireCategory(t, err, common.CategoryInvalidRequest)
}

func TestMoveTab_UnknownTargetSessionIsNotFound(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com")
	h.sessions.IsAttachedErr = ErrSessionNotFound

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 9})
	requireCategory(t, err, common.CategoryNotFound)
	got, _ := h.driver.Window(src.ID)
	assert.Len(t, got.Tabs, 1)
}

func TestMoveTab_UnavailableWhenDisconnected(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com")
	h.svc.handleEvent(context.Background(), Event{Type: EventDisconnected})

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 2})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestMoveTab_DriverErrorLeavesNoRow(t *testing.T) {
	h := newHarness(t)
	src := ownWindow(t, h, 1, "https://a.example.com")
	h.driver.MoveErr = ErrNotConnected

	_, err := h.svc.MoveTab(context.Background(), MoveTabRequest{SessionID: 1, WindowID: src.ID, TabID: src.Tabs[0].ID, ToSessionID: 2})
	assert.ErrorIs(t, err, ErrUnavailable)
	rows, err := h.store.ListOpenBySession(2)
	require.NoError(t, err)
	assert.Empty(t, rows)
}
