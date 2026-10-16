package browser

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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

func sampleTabs() []SavedTab {
	return []SavedTab{
		{URL: "https://a.example", Title: "A", Pinned: true},
		{URL: "https://b.example", Title: "B", Active: true},
	}
}

func getRow(t *testing.T, s *Store, id uint) BrowserWindow {
	t.Helper()
	var w BrowserWindow
	require.NoError(t, s.db.Where("id = ?", id).First(&w).Error)
	return w
}

func TestStore_TabsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	w := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a", Tabs: sampleTabs(), SnapshotAt: at}
	require.NoError(t, s.Add(w))

	got := getRow(t, s, w.ID)
	assert.Equal(t, sampleTabs(), got.Tabs)
	assert.True(t, got.SnapshotAt.Equal(at))
}

func TestStore_NilTabsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}
	require.NoError(t, s.Add(w))

	assert.Nil(t, getRow(t, s, w.ID).Tabs)
}

type legacyBrowserWindow struct {
	gorm.Model
	SessionID      uint
	State          WindowState
	DriverWindowID string
	LastFocusedAt  time.Time
}

func (legacyBrowserWindow) TableName() string { return "browser_windows" }

func TestStore_MigratedRowsDecodeWithoutTabs(t *testing.T) {
	database := testdb.New(t, &legacyBrowserWindow{})
	legacy := &legacyBrowserWindow{SessionID: 1, State: WindowMissing}
	require.NoError(t, database.Create(legacy).Error)
	require.NoError(t, database.Migrate(&BrowserWindow{}))
	s := NewStore(database)

	rows, err := s.ListRestorable(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].Tabs)
	assert.True(t, rows[0].SnapshotAt.IsZero())

	require.NoError(t, database.Exec("UPDATE browser_windows SET tabs = '' WHERE id = ?", legacy.ID).Error)
	rows, err = s.ListRestorable(1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Nil(t, rows[0].Tabs)
}

func TestStore_SaveTabs(t *testing.T) {
	s := newTestStore(t)
	open := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}
	require.NoError(t, s.Add(open))
	missing := &BrowserWindow{SessionID: 1, State: WindowMissing}
	require.NoError(t, s.Add(missing))
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.SaveTabs("a", sampleTabs(), at))
	require.NoError(t, s.SaveTabs("", sampleTabs(), at))

	got := getRow(t, s, open.ID)
	assert.Equal(t, sampleTabs(), got.Tabs)
	assert.True(t, got.SnapshotAt.Equal(at))
	assert.Nil(t, getRow(t, s, missing.ID).Tabs)
}

func TestStore_ListBySession(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowMissing}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowSnapshotted}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: "b"}))
	gone := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "c"}
	require.NoError(t, s.Add(gone))
	require.NoError(t, s.DeleteByDriverWindowID("c"))

	rows, err := s.ListBySession(1)
	require.NoError(t, err)
	var states []WindowState
	for _, r := range rows {
		states = append(states, r.State)
	}
	assert.ElementsMatch(t, []WindowState{WindowOpen, WindowMissing, WindowSnapshotted}, states)
}

func TestStore_ListRestorable(t *testing.T) {
	s := newTestStore(t)
	now := time.Now()
	older := &BrowserWindow{SessionID: 1, State: WindowMissing, LastFocusedAt: now.Add(-time.Hour)}
	newer := &BrowserWindow{SessionID: 1, State: WindowSnapshotted, LastFocusedAt: now}
	require.NoError(t, s.Add(older))
	require.NoError(t, s.Add(newer))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a", LastFocusedAt: now}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowMissing}))

	rows, err := s.ListRestorable(1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, newer.ID, rows[0].ID)
	assert.Equal(t, older.ID, rows[1].ID)
}

func TestStore_MarkSnapshotted(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}
	require.NoError(t, s.Add(w))
	other := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "b"}
	require.NoError(t, s.Add(other))
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.MarkSnapshotted(w.ID, sampleTabs(), at))

	untouched := getRow(t, s, other.ID)
	assert.Equal(t, WindowOpen, untouched.State)
	assert.Equal(t, "b", untouched.DriverWindowID)
	assert.Empty(t, untouched.Tabs)
	assert.True(t, untouched.SnapshotAt.IsZero())

	got := getRow(t, s, w.ID)
	assert.Equal(t, WindowSnapshotted, got.State)
	assert.Empty(t, got.DriverWindowID)
	assert.Equal(t, sampleTabs(), got.Tabs)
	assert.True(t, got.SnapshotAt.Equal(at))
}

func TestStore_Reopen(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowSnapshotted, Tabs: sampleTabs()}
	require.NoError(t, s.Add(w))
	other := &BrowserWindow{SessionID: 1, State: WindowMissing, Tabs: sampleTabs()}
	require.NoError(t, s.Add(other))
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)

	require.NoError(t, s.Reopen(w.ID, "z", at))

	untouched := getRow(t, s, other.ID)
	assert.Equal(t, WindowMissing, untouched.State)
	assert.Empty(t, untouched.DriverWindowID)
	assert.True(t, untouched.LastFocusedAt.IsZero())

	got, err := s.GetByDriverWindowID("z")
	require.NoError(t, err)
	assert.Equal(t, w.ID, got.ID)
	assert.Equal(t, WindowOpen, got.State)
	assert.True(t, got.LastFocusedAt.Equal(at))
	assert.Equal(t, sampleTabs(), got.Tabs)
}

func TestStore_ReopenWithoutDriverWindowIDIsNoop(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowSnapshotted, Tabs: sampleTabs()}
	require.NoError(t, s.Add(w))

	require.NoError(t, s.Reopen(w.ID, "", time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)))

	got := getRow(t, s, w.ID)
	assert.Equal(t, WindowSnapshotted, got.State)
	assert.True(t, got.LastFocusedAt.IsZero())
}

func TestStore_MarkMissingByDriverWindowID(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a", Tabs: sampleTabs()}
	require.NoError(t, s.Add(w))
	other := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "b"}
	require.NoError(t, s.Add(other))

	require.NoError(t, s.MarkMissingByDriverWindowID("a"))
	require.NoError(t, s.MarkMissingByDriverWindowID(""))

	got := getRow(t, s, w.ID)
	assert.Equal(t, WindowMissing, got.State)
	assert.Empty(t, got.DriverWindowID)
	assert.Equal(t, sampleTabs(), got.Tabs)
	assert.Equal(t, WindowOpen, getRow(t, s, other.ID).State)
}

func TestStore_DeleteBySession(t *testing.T) {
	s := newTestStore(t)
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "a"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowMissing}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 2, State: WindowOpen, DriverWindowID: "b"}))

	require.NoError(t, s.DeleteBySession(1))

	rows, err := s.ListBySession(1)
	require.NoError(t, err)
	assert.Empty(t, rows)
	rows, err = s.ListBySession(2)
	require.NoError(t, err)
	assert.Len(t, rows, 1)
}

func TestStore_ListMissing(t *testing.T) {
	s := newTestStore(t)
	a := &BrowserWindow{SessionID: 1, State: WindowMissing}
	b := &BrowserWindow{SessionID: 2, State: WindowMissing}
	require.NoError(t, s.Add(a))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "x"}))
	require.NoError(t, s.Add(&BrowserWindow{SessionID: 1, State: WindowSnapshotted}))
	require.NoError(t, s.Add(b))

	rows, err := s.ListMissing()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, a.ID, rows[0].ID)
	assert.Equal(t, b.ID, rows[1].ID)
}

func TestStore_MarkMissingExceptKeepsTabs(t *testing.T) {
	s := newTestStore(t)
	w := &BrowserWindow{SessionID: 1, State: WindowOpen, DriverWindowID: "gone", Tabs: sampleTabs()}
	require.NoError(t, s.Add(w))

	require.NoError(t, s.MarkMissingExcept(map[string]bool{}))

	got := getRow(t, s, w.ID)
	assert.Equal(t, WindowMissing, got.State)
	assert.Equal(t, sampleTabs(), got.Tabs)
}

func (s *Store) ListBySession(sessionID uint) ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("session_id = ?", sessionID).Order("id").Find(&rows).Error
	return rows, err
}
