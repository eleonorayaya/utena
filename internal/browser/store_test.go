package browser

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	return NewStore(testdb.New(t, &BrowserWindow{}, &BrowserState{}))
}

func TestStore_ListOpenBySessionOrdersByFocus(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a", LastFocusedAt: now.Add(-time.Hour)}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "b", LastFocusedAt: now}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowMissing}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: "c"}))

	rows, err := s.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "b", rows[0].DriverWindowID)
	assert.Equal(t, "a", rows[1].DriverWindowID)
}

func TestStore_GetByDriverWindowID(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 7, State: WindowOpen, DriverWindowID: "x"}))

	got, err := s.GetByDriverWindowID("x")
	require.NoError(t, err)
	assert.Equal(t, uint(7), got.SessionID)

	_, err = s.GetByDriverWindowID("nope")
	assert.ErrorIs(t, err, ErrWindowNotFound)

	_, err = s.GetByDriverWindowID("")
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestStore_OwnedDriverWindowIDs(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowMissing}))

	owned, err := s.OwnedDriverWindowIDs()
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"a": true}, owned)
}

func TestStore_ListOpen(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: "b"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 3, State: WindowMissing}))

	rows, err := s.ListOpen()
	require.NoError(t, err)
	got := map[string]uint{}
	for _, r := range rows {
		got[r.DriverWindowID] = r.SessionID
	}
	assert.Equal(t, map[string]uint{"a": 1, "b": 2}, got)
}

func TestStore_DeleteByDriverWindowID(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	require.NoError(t, s.DeleteByDriverWindowID("a"))
	_, err := s.GetByDriverWindowID("a")
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestStore_TouchFocused(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	require.NoError(t, s.TouchFocused("a", at))
	got, err := s.GetByDriverWindowID("a")
	require.NoError(t, err)
	assert.True(t, got.LastFocusedAt.Equal(at))
}

func TestStore_MarkMissingExcept(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "live"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "gone"}))

	require.NoError(t, s.MarkMissingExcept(map[string]bool{"live": true}))

	rows, err := s.ListOpenBySession(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "live", rows[0].DriverWindowID)
	_, err = s.GetByDriverWindowID("gone")
	assert.ErrorIs(t, err, ErrWindowNotFound)
}

func TestStore_RunIDRoundTrip(t *testing.T) {
	s := newTestStore(t)
	run, err := s.RunID()
	require.NoError(t, err)
	assert.Empty(t, run)

	require.NoError(t, s.SetRunID("a"))
	require.NoError(t, s.SetRunID("b"))
	run, err = s.RunID()
	require.NoError(t, err)
	assert.Equal(t, "b", run)
}
