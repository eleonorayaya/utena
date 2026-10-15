# Browser Core (PR 1) Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Route links opened from a utena session into that session's own Chrome window. This covers `utena open`, `$BROWSER` and the TUI's `o`. Parking and the window manager are left for later. Everything here works without AeroSpace.

**Architecture:** A new `internal/browser` domain package owns the session-to-window links (SQLite), the open/dedup/focus rules, and the `/browser` HTTP routes. It talks to browsers only through `browser.Driver`. `internal/chrome` implements that driver as a WebSocket endpoint (`/browser/chrome`) that an MV3 extension connects to. The CLI and TUI call `POST /browser/open` and fall back to `/usr/bin/open` on any failure.

**Tech Stack:** Go, chi, GORM/SQLite, `github.com/coder/websocket` (already a dependency), bubbletea, plain JS (Chrome MV3 service worker).

**Spec:** `docs/superpowers/specs/2026-10-15-browser-window-management-design.md`. This plan departs from the spec in a few small ways for PR 1. Task 12 records them in the spec:
- `browser.Driver` gains `FocusWindow`, which lets Chrome focus a window. AeroSpace follows macOS focus, so `wm.Driver` won't need a focus method.
- Rows store `DriverWindowID` as a hint, so a daemon restart while Chrome keeps running keeps ownership. Rows whose window is gone on `Connected` become `missing`, with the ID cleared. Restoring them is PR 3.
- Links from outside any session (`session_id` 0) go to an unowned Chrome window through the daemon, not straight to `/usr/bin/open`. That keeps personal links out of session windows.
- There's no `--browser` flag yet. Chrome is the only driver, and with no extension connected the daemon returns 503, which triggers the fallback.
- The extension has no pinned `key`. Without a native host, nothing needs a fixed extension ID. The WebSocket endpoint accepts only `chrome-extension://` origins, which web pages cannot forge.

**Rules:** Run everything through `task` from the repo root (`task test`, `task lint`, `task fmt`). Never call `go` directly. Don't add code comments. Commit after every task, ending each message with:

```
Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>
```

To run one package's tests while iterating, `task test` is the only sanctioned entry point. It is cached by `sources`, so run `task test --force` if it reports "up to date".

---

### Task 1: The `browser` driver contract and fakes

**Files:**
- Create: `internal/browser/driver.go`
- Create: `internal/browser/testing.go`

**Step 1: Write `internal/browser/driver.go`**

```go
package browser

import (
	"context"
	"errors"
	"net/http"
)

var ErrNotConnected = errors.New("browser not connected")

type Driver interface {
	ListWindows(ctx context.Context) ([]Window, error)
	CreateWindow(ctx context.Context, urls []string, focused bool) (Window, error)
	OpenTab(ctx context.Context, windowID, url string) (Tab, error)
	ActivateTab(ctx context.Context, windowID, tabID string) error
	FocusWindow(ctx context.Context, windowID string) error
	Subscribe(ctx context.Context, onEvent func(Event)) error
}

type HTTPDriver interface {
	Name() string
	Handler() http.Handler
}

type Window struct {
	ID      string `json:"id"`
	Focused bool   `json:"focused"`
	Tabs    []Tab  `json:"tabs"`
}

type Tab struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
}

type EventType string

const (
	EventConnected     EventType = "connected"
	EventDisconnected  EventType = "disconnected"
	EventWindowCreated EventType = "window_created"
	EventWindowRemoved EventType = "window_removed"
	EventTabsChanged   EventType = "tabs_changed"
	EventFocusChanged  EventType = "focus_changed"
)

type Event struct {
	Type     EventType
	WindowID string
}
```

**Step 2: Write `internal/browser/testing.go`.** It holds an in-memory browser and a fake session resolver. Window IDs are `"w1"`, `"w2"` and so on, and tab IDs are `"t1"`, `"t2"` and so on.

```go
package browser

import (
	"context"
	"fmt"
	"sync"
)

type FakeDriver struct {
	mu       sync.Mutex
	windows  []Window
	nextID   int
	focused  []string
	onEvent  func(Event)
	ListErr  error
}

var _ Driver = (*FakeDriver)(nil)

func NewFakeDriver() *FakeDriver {
	return &FakeDriver{}
}

func (f *FakeDriver) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s%d", prefix, f.nextID)
}

func (f *FakeDriver) AddWindow(urls ...string) Window {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addWindowLocked(urls)
}

func (f *FakeDriver) addWindowLocked(urls []string) Window {
	w := Window{ID: f.id("w")}
	for i, u := range urls {
		w.Tabs = append(w.Tabs, Tab{ID: f.id("t"), URL: u, Active: i == len(urls)-1})
	}
	f.windows = append(f.windows, w)
	return w
}

func (f *FakeDriver) RemoveWindow(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, w := range f.windows {
		if w.ID == id {
			f.windows = append(f.windows[:i], f.windows[i+1:]...)
			return
		}
	}
}

func (f *FakeDriver) Window(id string) (Window, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.windows {
		if w.ID == id {
			return w, true
		}
	}
	return Window{}, false
}

func (f *FakeDriver) Focused() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.focused...)
}

func (f *FakeDriver) ListWindows(ctx context.Context) ([]Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	out := make([]Window, len(f.windows))
	for i, w := range f.windows {
		w.Tabs = append([]Tab(nil), w.Tabs...)
		out[i] = w
	}
	return out, nil
}

func (f *FakeDriver) CreateWindow(ctx context.Context, urls []string, focused bool) (Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.addWindowLocked(urls)
	if focused {
		f.focused = append(f.focused, w.ID)
	}
	return w, nil
}

func (f *FakeDriver) OpenTab(ctx context.Context, windowID, url string) (Tab, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		if f.windows[i].ID != windowID {
			continue
		}
		for j := range f.windows[i].Tabs {
			f.windows[i].Tabs[j].Active = false
		}
		t := Tab{ID: f.id("t"), URL: url, Active: true}
		f.windows[i].Tabs = append(f.windows[i].Tabs, t)
		return t, nil
	}
	return Tab{}, fmt.Errorf("no window %s", windowID)
}

func (f *FakeDriver) ActivateTab(ctx context.Context, windowID, tabID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		if f.windows[i].ID != windowID {
			continue
		}
		for j := range f.windows[i].Tabs {
			f.windows[i].Tabs[j].Active = f.windows[i].Tabs[j].ID == tabID
		}
		return nil
	}
	return fmt.Errorf("no window %s", windowID)
}

func (f *FakeDriver) FocusWindow(ctx context.Context, windowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = append(f.focused, windowID)
	return nil
}

func (f *FakeDriver) Subscribe(ctx context.Context, onEvent func(Event)) error {
	f.mu.Lock()
	f.onEvent = onEvent
	f.mu.Unlock()
	<-ctx.Done()
	f.mu.Lock()
	f.onEvent = nil
	f.mu.Unlock()
	return ctx.Err()
}

func (f *FakeDriver) Emit(ev Event) bool {
	f.mu.Lock()
	onEvent := f.onEvent
	f.mu.Unlock()
	if onEvent == nil {
		return false
	}
	onEvent(ev)
	return true
}

type FakeSessions struct {
	mu        sync.Mutex
	Attached  uint
	Activated []uint
	ActivateErr error
}

func (f *FakeSessions) IsAttached(ctx context.Context, sessionID uint) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Attached == sessionID, nil
}

func (f *FakeSessions) Activate(ctx context.Context, sessionID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ActivateErr != nil {
		return f.ActivateErr
	}
	f.Activated = append(f.Activated, sessionID)
	f.Attached = sessionID
	return nil
}
```

`FakeSessions` refers to the `Sessions` interface, which is defined in Task 4. Until then this file compiles on its own, because `FakeSessions` isn't asserted against the interface yet. Task 4 adds `var _ Sessions = (*FakeSessions)(nil)`.

**Step 3: Format and build**

Run: `task fmt && task lint`
Expected: it passes. (`task lint` type-checks the package.)

**Step 4: Commit**

```bash
git add internal/browser
git commit -m "feat(browser): driver contract and fakes"
```

---

### Task 2: URL prefix matching

**Files:**
- Create: `internal/browser/match.go`
- Test: `internal/browser/match_test.go`

**Step 1: Write the failing test**

```go
package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchTab(t *testing.T) {
	pr := "https://github.com/o/r/pull/123"
	tests := []struct {
		name   string
		tabs   []string
		url    string
		wantID string
	}{
		{"exact", []string{pr}, pr, "0"},
		{"tab is deeper", []string{pr + "/files"}, pr, "0"},
		{"trailing slash", []string{pr + "/"}, pr, "0"},
		{"query and fragment ignored", []string{pr + "/files?w=1#diff"}, pr + "#top", "0"},
		{"segment boundary", []string{"https://github.com/o/r/pull/1234"}, pr, ""},
		{"request is deeper", []string{pr}, pr + "/files", ""},
		{"other host", []string{"https://gitlab.com/o/r/pull/123"}, pr, ""},
		{"host case-insensitive", []string{"https://GitHub.com/o/r/pull/123"}, pr, "0"},
		{"exact beats deeper", []string{pr + "/files", pr}, pr, "1"},
		{"root request matches nothing", []string{"https://github.com/x"}, "https://github.com/", ""},
		{"unparseable tab skipped", []string{"::bad", pr}, pr, "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tabs := make([]Tab, len(tt.tabs))
			for i, u := range tt.tabs {
				tabs[i] = Tab{ID: string(rune('0' + i)), URL: u}
			}
			got, ok := MatchTab(tabs, tt.url)
			if tt.wantID == "" {
				assert.False(t, ok)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}
```

**Step 2: Run the test to confirm it fails**

Run: `task test --force`
Expected: it FAILS to compile with `undefined: MatchTab`.

**Step 3: Implement it**

```go
package browser

import (
	"net/url"
	"strings"
)

func MatchTab(tabs []Tab, rawURL string) (Tab, bool) {
	want, ok := pathKey(rawURL)
	if !ok || len(want.segments) == 0 {
		return Tab{}, false
	}
	var best Tab
	found := false
	for _, t := range tabs {
		have, ok := pathKey(t.URL)
		if !ok || have.host != want.host || !hasSegmentPrefix(have.segments, want.segments) {
			continue
		}
		if len(have.segments) == len(want.segments) {
			return t, true
		}
		if !found {
			best, found = t, true
		}
	}
	return best, found
}

type urlKey struct {
	host     string
	segments []string
}

func pathKey(raw string) (urlKey, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return urlKey{}, false
	}
	var segs []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return urlKey{host: strings.ToLower(u.Host), segments: segs}, true
}

func hasSegmentPrefix(have, want []string) bool {
	if len(have) < len(want) {
		return false
	}
	for i := range want {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}
```

**Step 4: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS.

**Step 5: Commit**

```bash
git add internal/browser/match.go internal/browser/match_test.go
git commit -m "feat(browser): prefix-match existing tabs"
```

---

### Task 3: The `BrowserWindow` model and store

**Files:**
- Create: `internal/browser/window.go`
- Create: `internal/browser/store.go`
- Test: `internal/browser/store_test.go`

**Step 1: Write the model, `internal/browser/window.go`**

```go
package browser

import (
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"gorm.io/gorm"
)

var ErrWindowNotFound = common.NewNotFound("browser window not found")

type WindowState string

const (
	WindowOpen    WindowState = "open"
	WindowMissing WindowState = "missing"
)

type BrowserWindow struct {
	gorm.Model
	SessionID      uint        `json:"session_id" gorm:"index"`
	State          WindowState `json:"state"`
	DriverWindowID string      `json:"driver_window_id" gorm:"index"`
	LastFocusedAt  time.Time   `json:"last_focused_at"`
}
```

**Step 2: Write the failing store test, `internal/browser/store_test.go`**

```go
package browser

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *Store {
	return NewStore(testdb.New(t, &BrowserWindow{}))
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
```

**Step 3: Run the test to confirm it fails**

Run: `task test --force`
Expected: it FAILS with `undefined: Store` / `NewStore`.

**Step 4: Implement `internal/browser/store.go`**

```go
package browser

import (
	"errors"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type Store struct {
	db db.Database
}

func NewStore(database db.Database) *Store {
	return &Store{db: database}
}

func (s *Store) Add(w *BrowserWindow) error {
	return s.db.Create(w).Error
}

func (s *Store) GetByDriverWindowID(id string) (*BrowserWindow, error) {
	if id == "" {
		return nil, ErrWindowNotFound
	}
	var w BrowserWindow
	if err := s.db.Where("driver_window_id = ? AND state = ?", id, WindowOpen).First(&w).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrWindowNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *Store) ListOpenBySession(sessionID uint) ([]BrowserWindow, error) {
	var rows []BrowserWindow
	err := s.db.Where("session_id = ? AND state = ?", sessionID, WindowOpen).
		Order("last_focused_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (s *Store) OwnedDriverWindowIDs() (map[string]bool, error) {
	var rows []BrowserWindow
	if err := s.db.Where("state = ?", WindowOpen).Find(&rows).Error; err != nil {
		return nil, err
	}
	owned := make(map[string]bool, len(rows))
	for _, r := range rows {
		owned[r.DriverWindowID] = true
	}
	return owned, nil
}

func (s *Store) DeleteByDriverWindowID(id string) error {
	if id == "" {
		return nil
	}
	return s.db.Where("driver_window_id = ? AND state = ?", id, WindowOpen).Delete(&BrowserWindow{}).Error
}

func (s *Store) TouchFocused(id string, at time.Time) error {
	if id == "" {
		return nil
	}
	return s.db.Model(&BrowserWindow{}).
		Where("driver_window_id = ? AND state = ?", id, WindowOpen).
		Update("last_focused_at", at).Error
}

func (s *Store) MarkMissingExcept(live map[string]bool) error {
	var rows []BrowserWindow
	if err := s.db.Where("state = ?", WindowOpen).Find(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		if live[r.DriverWindowID] {
			continue
		}
		if err := s.db.Model(&BrowserWindow{}).Where("id = ?", r.ID).
			Updates(map[string]any{"state": WindowMissing, "driver_window_id": ""}).Error; err != nil {
			return err
		}
	}
	return nil
}
```

**Step 5: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS.

**Step 6: Commit**

```bash
git add internal/browser/window.go internal/browser/store.go internal/browser/store_test.go
git commit -m "feat(browser): BrowserWindow model and store"
```

---

### Task 4: The service, part 1: `Open`

**Files:**
- Create: `internal/browser/service.go`
- Modify: `internal/browser/testing.go` (add the `Sessions` assertion)
- Test: `internal/browser/service_test.go`

The rules being implemented:
- No connection gives `ErrUnavailable`.
- `session_id == 0`: use the most recently focused **unowned** live window, otherwise any unowned window, otherwise create one. Always focus it.
- `session_id != 0`: if `Activate` is set and the session isn't attached, activate it first. The target is the session's most recently focused `open` row whose window is live. Otherwise create a window and insert a row. Focus only if the session is attached (after any activation).
- In the target window, `MatchTab` hit means `ActivateTab`, otherwise `OpenTab`.

**Step 1: Write the failing tests, `internal/browser/service_test.go`**

```go
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
```

**Step 2: Run the tests to confirm they fail**

Run: `task test --force`
Expected: they FAIL with `undefined: NewService`, `OpenRequest`, and so on.

**Step 3: Implement `internal/browser/service.go`**

```go
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"
)

var ErrUnavailable = errors.New("browser unavailable")

type Sessions interface {
	IsAttached(ctx context.Context, sessionID uint) (bool, error)
	Activate(ctx context.Context, sessionID uint) error
}

type OpenRequest struct {
	SessionID uint   `json:"session_id"`
	URL       string `json:"url"`
	Activate  bool   `json:"activate"`
}

func (r *OpenRequest) Bind(*http.Request) error {
	u, err := url.Parse(r.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("url must be absolute: %q", r.URL)
	}
	return nil
}

type OpenResult struct {
	WindowID string `json:"window_id"`
	TabID    string `json:"tab_id"`
	Created  bool   `json:"created"`
	Focused  bool   `json:"focused"`
}

type Service struct {
	driver   Driver
	store    *Store
	sessions Sessions
	now      func() time.Time

	mu                  sync.Mutex
	connected           bool
	lastFocusedWindowID string
}

func NewService(driver Driver, store *Store, sessions Sessions) *Service {
	return &Service{driver: driver, store: store, sessions: sessions, now: time.Now}
}

func (s *Service) Connected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.connected
}

func (s *Service) Open(ctx context.Context, req OpenRequest) (OpenResult, error) {
	if !s.Connected() {
		return OpenResult{}, ErrUnavailable
	}

	focus := true
	if req.SessionID != 0 {
		attached, err := s.sessions.IsAttached(ctx, req.SessionID)
		if err != nil {
			return OpenResult{}, err
		}
		if req.Activate && !attached {
			if err := s.sessions.Activate(ctx, req.SessionID); err != nil {
				return OpenResult{}, err
			}
			attached = true
		}
		focus = attached
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected {
		return OpenResult{}, ErrUnavailable
	}

	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return OpenResult{}, err
	}
	target, found, err := s.targetWindow(req.SessionID, live)
	if err != nil {
		return OpenResult{}, err
	}

	var res OpenResult
	switch {
	case !found:
		w, err := s.driver.CreateWindow(ctx, []string{req.URL}, focus)
		if err != nil {
			return OpenResult{}, err
		}
		if req.SessionID != 0 {
			row := &BrowserWindow{SessionID: req.SessionID, State: WindowOpen, DriverWindowID: w.ID, LastFocusedAt: s.now()}
			if err := s.store.Add(row); err != nil {
				return OpenResult{}, err
			}
		}
		res = OpenResult{WindowID: w.ID, Created: true}
		if len(w.Tabs) > 0 {
			res.TabID = w.Tabs[0].ID
		}
	default:
		res.WindowID = target.ID
		if tab, ok := MatchTab(target.Tabs, req.URL); ok {
			if err := s.driver.ActivateTab(ctx, target.ID, tab.ID); err != nil {
				return OpenResult{}, err
			}
			res.TabID = tab.ID
		} else {
			tab, err := s.driver.OpenTab(ctx, target.ID, req.URL)
			if err != nil {
				return OpenResult{}, err
			}
			res.TabID = tab.ID
		}
	}

	if focus {
		if err := s.driver.FocusWindow(ctx, res.WindowID); err != nil {
			slog.WarnContext(ctx, "browser: focus window failed", "window", res.WindowID, "error", err)
		} else {
			res.Focused = true
		}
	}
	return res, nil
}

func (s *Service) targetWindow(sessionID uint, live []Window) (Window, bool, error) {
	byID := make(map[string]Window, len(live))
	for _, w := range live {
		byID[w.ID] = w
	}

	if sessionID == 0 {
		owned, err := s.store.OwnedDriverWindowIDs()
		if err != nil {
			return Window{}, false, err
		}
		if w, ok := byID[s.lastFocusedWindowID]; ok && !owned[w.ID] {
			return w, true, nil
		}
		for _, w := range live {
			if !owned[w.ID] {
				return w, true, nil
			}
		}
		return Window{}, false, nil
	}

	rows, err := s.store.ListOpenBySession(sessionID)
	if err != nil {
		return Window{}, false, err
	}
	for _, r := range rows {
		if w, ok := byID[r.DriverWindowID]; ok {
			return w, true, nil
		}
	}
	return Window{}, false, nil
}

func (s *Service) handleEvent(ctx context.Context, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch ev.Type {
	case EventConnected:
		s.connected = true
	case EventDisconnected:
		s.connected = false
	case EventFocusChanged:
		if ev.WindowID == "" {
			return
		}
		s.lastFocusedWindowID = ev.WindowID
		if err := s.store.TouchFocused(ev.WindowID, s.now()); err != nil {
			slog.WarnContext(ctx, "browser: record focus failed", "window", ev.WindowID, "error", err)
		}
	}
}
```

`sessions.IsAttached` and `sessions.Activate` deliberately run **outside** `s.mu`. `ActivateSession` does worktree and branch sync synchronously. It also publishes `session.activated` on the synchronous eventbus, so a later PR that subscribes `browser` to that event would deadlock on a lock this goroutine already holds.

Then add this to `internal/browser/testing.go`, just below the `FakeSessions` type:

```go
var _ Sessions = (*FakeSessions)(nil)
```

**Step 4: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS.

**Step 5: Commit**

```bash
git add internal/browser
git commit -m "feat(browser): open links in a session's window"
```

---

### Task 5: The service, part 2: events, resync and ownership

**Files:**
- Modify: `internal/browser/service.go`
- Test: `internal/browser/service_events_test.go`

The rules:
- `Connected`: mark connected, then **resync**. List the windows, call `store.MarkMissingExcept(liveIDs)`, and set `lastFocusedWindowID` from the window with `Focused`.
- `WindowCreated`: if a row already has that ID, do nothing, because utena created it. Otherwise the parent is `lastFocusedWindowID`. If Chrome sent `onFocusChanged(new)` before `onCreated(new)`, so that `lastFocusedWindowID` is the new window itself, the parent is `prevFocusedWindowID` instead. If the parent belongs to an `open` row, insert a row for the new window with the same `SessionID`. Chrome's event order for cmd-N hasn't been checked, so both orders must work.
- `WindowRemoved`: delete its row (a deliberate close; telling a quit from a close is PR 3).
- `OnAppStart` runs `followEvents`, which retries `driver.Subscribe` every 2 seconds the same way `mux.Service.followEvents` does. When the driver stream ends, it marks the service disconnected.

**Step 1: Write the failing tests**

```go
package browser

import (
	"context"
	"testing"
	"time"

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
```

**Step 2: Run the tests to confirm they fail**

Run: `task test --force`
Expected: they FAIL. The resync, created and removed cases fail on assertions, and `followEvents` is undefined.

**Step 3: Implement it.** In `internal/browser/service.go`, add the field `prevFocusedWindowID string` to `Service`, next to `lastFocusedWindowID`. Then replace `handleEvent` and add the methods below:

```go
func (s *Service) OnAppStart(ctx context.Context) error {
	go s.followEvents(ctx)
	return nil
}

func (s *Service) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *Service) followEvents(ctx context.Context) {
	lastErr := ""
	for ctx.Err() == nil {
		err := s.driver.Subscribe(ctx, func(ev Event) {
			lastErr = ""
			s.handleEvent(ctx, ev)
		})
		s.handleEvent(ctx, Event{Type: EventDisconnected})
		if ctx.Err() != nil {
			return
		}
		if err != nil && err.Error() != lastErr {
			lastErr = err.Error()
			slog.Warn("browser event stream unavailable; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (s *Service) handleEvent(ctx context.Context, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	switch ev.Type {
	case EventConnected:
		s.connected = true
		err = s.resync(ctx)
	case EventDisconnected:
		s.connected = false
	case EventWindowCreated:
		err = s.adopt(ev.WindowID)
	case EventWindowRemoved:
		err = s.store.DeleteByDriverWindowID(ev.WindowID)
	case EventFocusChanged:
		if ev.WindowID == "" {
			return
		}
		if ev.WindowID != s.lastFocusedWindowID {
			s.prevFocusedWindowID = s.lastFocusedWindowID
			s.lastFocusedWindowID = ev.WindowID
		}
		err = s.store.TouchFocused(ev.WindowID, s.now())
	}
	if err != nil {
		slog.WarnContext(ctx, "browser event handler failed", "type", ev.Type, "window", ev.WindowID, "error", err)
	}
}

func (s *Service) resync(ctx context.Context) error {
	windows, err := s.driver.ListWindows(ctx)
	if err != nil {
		return err
	}
	live := make(map[string]bool, len(windows))
	for _, w := range windows {
		live[w.ID] = true
		if w.Focused {
			s.lastFocusedWindowID = w.ID
		}
	}
	return s.store.MarkMissingExcept(live)
}

func (s *Service) adopt(windowID string) error {
	if _, err := s.store.GetByDriverWindowID(windowID); err == nil {
		return nil
	} else if !errors.Is(err, ErrWindowNotFound) {
		return err
	}
	parentID := s.lastFocusedWindowID
	if parentID == windowID {
		parentID = s.prevFocusedWindowID
	}
	parent, err := s.store.GetByDriverWindowID(parentID)
	if errors.Is(err, ErrWindowNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.store.Add(&BrowserWindow{SessionID: parent.SessionID, State: WindowOpen, DriverWindowID: windowID, LastFocusedAt: s.now()})
}
```

> [!NOTE]
> `Open` holds `s.mu` while it calls the driver. Event delivery must therefore never be the goroutine that resolves driver responses, or a `WindowCreated` that arrives during `CreateWindow` would deadlock. The Chrome driver (Task 7) delivers events on the `Subscribe` goroutine and resolves responses on its read loop, so this holds. It also gives the ordering `TestWindowCreated_IgnoresWindowsUtenaCreated` depends on: the row is inserted before the event is handled.

**Step 4: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS.

**Step 5: Commit**

```bash
git add internal/browser
git commit -m "feat(browser): resync, window ownership and event loop"
```

---

### Task 6: The controller, router, module and client

**Files:**
- Create: `internal/browser/controller.go`, `router.go`, `module.go`, `client.go`
- Test: `internal/browser/router_test.go`

**Step 1: Write the failing test, `internal/browser/router_test.go`**

```go
package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpFake struct {
	*FakeDriver
	hits int
}

func (h *httpFake) Name() string { return "fake" }
func (h *httpFake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.hits++ })
}

func newTestServer(t *testing.T) (*httptest.Server, *Module, *httpFake) {
	t.Helper()
	driver := &httpFake{FakeDriver: NewFakeDriver()}
	m := NewModule(driver, &FakeSessions{}, testdb.New(t, &BrowserWindow{}))
	srv := httptest.NewServer(m.Routes())
	t.Cleanup(srv.Close)
	return srv, m, driver
}

func TestRouter_OpenReturns503WhenDisconnected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	_, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{SessionID: 1, URL: prURL})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestRouter_OpenSucceedsWhenConnected(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})

	res, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
}

func TestRouter_OpenRejectsRelativeURL(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	_, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{URL: "/relative"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnavailable)
}

func TestRouter_MountsDriverHandler(t *testing.T) {
	srv, _, driver := newTestServer(t)
	res, err := srv.Client().Get(srv.URL + "/fake")
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, 1, driver.hits)
}

func TestRouter_Status(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	res, err := srv.Client().Get(srv.URL + "/status")
	require.NoError(t, err)
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}
```

`RequestOpen` takes the base URL *with* the `/browser` prefix stripped by the caller. The test server serves the module's router at its root, so the client must call `baseURL + "/open"`. The production callers pass `http://localhost:<port>/browser`.

**Step 2: Run the test to confirm it fails**

Run: `task test --force`
Expected: it FAILS with `undefined: NewModule`, `RequestOpen`.

**Step 3: Implement the four files**

`internal/browser/controller.go`:

```go
package browser

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/render"
)

type Controller struct {
	service *Service
}

func NewController(service *Service) *Controller {
	return &Controller{service: service}
}

func (c *Controller) HandleOpen(w http.ResponseWriter, r *http.Request) {
	req := &OpenRequest{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 15*time.Second)
	defer cancel()

	res, err := c.service.Open(ctx, *req)
	if errors.Is(err, ErrUnavailable) {
		render.Status(r, http.StatusServiceUnavailable)
		render.JSON(w, r, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	render.JSON(w, r, res)
}

func (c *Controller) HandleStatus(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]bool{"connected": c.service.Connected()})
}
```

`context.WithoutCancel` matters here. With `activate: true`, the TUI popup is killed as soon as tuios reattaches, and the open must still finish after that.

> [!IMPORTANT]
> **Timeout ordering.** The server's budget is 15 seconds. Both clients (the CLI in Task 10 and the TUI in Task 11) wait **20 seconds** before falling back to `/usr/bin/open`. If a client gave up first, the server would still finish the open, and the fallback would open the link a second time.

`internal/browser/router.go`:

```go
package browser

import "github.com/go-chi/chi/v5"

type Router struct {
	controller *Controller
	driver     Driver
}

func NewRouter(controller *Controller, driver Driver) *Router {
	return &Router{controller: controller, driver: driver}
}

func (rt *Router) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/open", rt.controller.HandleOpen)
	r.Get("/status", rt.controller.HandleStatus)
	if hd, ok := rt.driver.(HTTPDriver); ok {
		r.Handle("/"+hd.Name(), hd.Handler())
	}
	return r
}
```

`internal/browser/module.go`:

```go
package browser

import (
	"context"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/go-chi/chi/v5"
)

type Module struct {
	Service    *Service
	Controller *Controller
	Router     *Router
}

func NewModule(driver Driver, sessions Sessions, database db.Database) *Module {
	service := NewService(driver, NewStore(database), sessions)
	controller := NewController(service)
	return &Module{Service: service, Controller: controller, Router: NewRouter(controller, driver)}
}

func (m *Module) OnAppStart(ctx context.Context) error {
	return m.Service.OnAppStart(ctx)
}

func (m *Module) OnAppEnd(ctx context.Context) error {
	return m.Service.OnAppEnd(ctx)
}

func (m *Module) Routes() chi.Router {
	return m.Router.Routes()
}

func (m *Module) Models() []any {
	return []any{&BrowserWindow{}}
}
```

`internal/browser/client.go`:

```go
package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func RequestOpen(ctx context.Context, client *http.Client, baseURL string, req OpenRequest) (OpenResult, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return OpenResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/open", bytes.NewReader(body))
	if err != nil {
		return OpenResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := client.Do(httpReq)
	if err != nil {
		return OpenResult{}, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusServiceUnavailable {
		return OpenResult{}, ErrUnavailable
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return OpenResult{}, fmt.Errorf("browser open: status %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out OpenResult
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return OpenResult{}, err
	}
	return out, nil
}
```

**Step 4: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS.

**Step 5: Commit**

```bash
git add internal/browser
git commit -m "feat(browser): /browser routes, module and client"
```

---

### Task 7: The Chrome driver (`internal/chrome`)

**Files:**
- Create: `internal/chrome/protocol.go`, `internal/chrome/driver.go`
- Test: `internal/chrome/driver_test.go`

**The wire protocol** is JSON text frames on `/browser/chrome`. Chrome IDs are integers on the wire and strings in `browser.*`.

| Direction | Shape |
|---|---|
| extension to daemon, first frame | `{"type":"hello","protocol":1,"browser":"chrome"}` |
| daemon to extension | `{"type":"req","id":N,"method":M,"params":{…}}` |
| extension to daemon | `{"type":"res","id":N,"result":{…}}` or `{"type":"res","id":N,"error":"…"}` |
| extension to daemon | `{"type":"event","event":E,"data":{"windowId":N}}` |
| extension to daemon | `{"type":"ping"}` (keepalive, ignored) |

Methods: `windows.list` returns `{windows:[window]}`. `windows.create {urls,focused}` returns `window`. `windows.focus {windowId}`. `tabs.create {windowId,url}` returns `tab`. `tabs.activate {windowId,tabId}`.
Events: `window.created`, `window.removed`, `window.focused` (where `windowId: -1` means none), `tabs.changed`.

**Step 1: Write `internal/chrome/protocol.go`**

```go
package chrome

import (
	"encoding/json"
	"strconv"

	"github.com/eleonorayaya/utena/internal/browser"
)

const ProtocolVersion = 1

type message struct {
	Type     string          `json:"type"`
	ID       int64           `json:"id,omitempty"`
	Method   string          `json:"method,omitempty"`
	Params   any             `json:"params,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
	Event    string          `json:"event,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	Protocol int             `json:"protocol,omitempty"`
	Browser  string          `json:"browser,omitempty"`
}

type wireTab struct {
	ID     int64  `json:"id"`
	URL    string `json:"url"`
	Title  string `json:"title"`
	Active bool   `json:"active"`
	Pinned bool   `json:"pinned"`
}

type wireWindow struct {
	ID      int64     `json:"id"`
	Focused bool      `json:"focused"`
	Tabs    []wireTab `json:"tabs"`
}

type windowEventData struct {
	WindowID *int64 `json:"windowId"`
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func (t wireTab) toTab() browser.Tab {
	return browser.Tab{ID: formatID(t.ID), URL: t.URL, Title: t.Title, Active: t.Active, Pinned: t.Pinned}
}

func (w wireWindow) toWindow() browser.Window {
	out := browser.Window{ID: formatID(w.ID), Focused: w.Focused, Tabs: make([]browser.Tab, len(w.Tabs))}
	for i, t := range w.Tabs {
		out.Tabs[i] = t.toTab()
	}
	return out
}

func translateEvent(m message) (browser.Event, bool) {
	var d windowEventData
	_ = json.Unmarshal(m.Data, &d)
	id := ""
	if d.WindowID != nil && *d.WindowID >= 0 {
		id = formatID(*d.WindowID)
	}
	switch m.Event {
	case "window.created":
		return browser.Event{Type: browser.EventWindowCreated, WindowID: id}, id != ""
	case "window.removed":
		return browser.Event{Type: browser.EventWindowRemoved, WindowID: id}, id != ""
	case "window.focused":
		return browser.Event{Type: browser.EventFocusChanged, WindowID: id}, true
	case "tabs.changed":
		return browser.Event{Type: browser.EventTabsChanged, WindowID: id}, id != ""
	}
	return browser.Event{}, false
}
```

**Step 2: Write the failing tests, `internal/chrome/driver_test.go`**

```go
package chrome

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type incoming struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type fakeExt struct {
	ws     *websocket.Conn
	handle func(method string, params json.RawMessage) (any, string)
}

func dial(t *testing.T, srv *httptest.Server, origin string) (*websocket.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}}})
	return ws, err
}

func startExt(t *testing.T, srv *httptest.Server, protocol int, handle func(string, json.RawMessage) (any, string)) *fakeExt {
	t.Helper()
	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws, handle: handle}
	ext.send(t, map[string]any{"type": "hello", "protocol": protocol, "browser": "chrome"})
	go ext.serve()
	return ext
}

func (e *fakeExt) send(t *testing.T, v any) {
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, e.ws.Write(context.Background(), websocket.MessageText, data))
}

func (e *fakeExt) serve() {
	for {
		_, data, err := e.ws.Read(context.Background())
		if err != nil {
			return
		}
		var m incoming
		if json.Unmarshal(data, &m) != nil || m.Type != "req" || e.handle == nil {
			continue
		}
		result, errMsg := e.handle(m.Method, m.Params)
		out := map[string]any{"type": "res", "id": m.ID}
		if errMsg != "" {
			out["error"] = errMsg
		} else {
			out["result"] = result
		}
		data, _ = json.Marshal(out)
		_ = e.ws.Write(context.Background(), websocket.MessageText, data)
	}
}

type recorder struct {
	mu     sync.Mutex
	events []browser.Event
}

func (r *recorder) add(ev browser.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) has(want browser.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev == want {
			return true
		}
	}
	return false
}

func newDriver(t *testing.T) (*Driver, *httptest.Server, *recorder) {
	t.Helper()
	d := NewDriver()
	srv := httptest.NewServer(d.Handler())
	t.Cleanup(srv.Close)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = d.Subscribe(ctx, rec.add) }()
	return d, srv, rec
}

func waitConnected(t *testing.T, rec *recorder) {
	t.Helper()
	require.Eventually(t, func() bool { return rec.has(browser.Event{Type: browser.EventConnected}) }, time.Second, 5*time.Millisecond)
}

func TestDriver_RejectsNonExtensionOrigin(t *testing.T) {
	_, srv, _ := newDriver(t)
	_, err := dial(t, srv, "https://evil.example.com")
	assert.Error(t, err)
}

func TestDriver_CallWithoutConnection(t *testing.T) {
	d, _, _ := newDriver(t)
	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
}

func TestDriver_RejectsProtocolMismatch(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion+1, nil)
	time.Sleep(50 * time.Millisecond)
	assert.False(t, rec.has(browser.Event{Type: browser.EventConnected}))
	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
}

func TestDriver_ListWindows(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(method string, _ json.RawMessage) (any, string) {
		assert.Equal(t, "windows.list", method)
		return map[string]any{"windows": []wireWindow{{ID: 4, Focused: true, Tabs: []wireTab{{ID: 9, URL: "https://x", Active: true}}}}}, ""
	})
	waitConnected(t, rec)

	windows, err := d.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []browser.Window{{ID: "4", Focused: true, Tabs: []browser.Tab{{ID: "9", URL: "https://x", Active: true}}}}, windows)
}

func TestDriver_SendsTypedParams(t *testing.T) {
	d, srv, rec := newDriver(t)
	var got map[string]any
	startExt(t, srv, ProtocolVersion, func(method string, params json.RawMessage) (any, string) {
		_ = json.Unmarshal(params, &got)
		return wireTab{ID: 2, URL: "https://y"}, ""
	})
	waitConnected(t, rec)

	tab, err := d.OpenTab(context.Background(), "4", "https://y")
	require.NoError(t, err)
	assert.Equal(t, "2", tab.ID)
	assert.Equal(t, float64(4), got["windowId"])
	assert.Equal(t, "https://y", got["url"])
}

func TestDriver_ErrorResponse(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) { return nil, "No window with id: 4." })
	waitConnected(t, rec)

	err := d.FocusWindow(context.Background(), "4")
	assert.ErrorContains(t, err, "No window with id")
}

func TestDriver_TranslatesEvents(t *testing.T) {
	_, srv, rec := newDriver(t)
	ext := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	ext.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": 5}})
	ext.send(t, map[string]any{"type": "event", "event": "window.focused", "data": map[string]any{"windowId": -1}})

	assert.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventWindowCreated, WindowID: "5"}) &&
			rec.has(browser.Event{Type: browser.EventFocusChanged, WindowID: ""})
	}, time.Second, 5*time.Millisecond)
}

func TestDriver_DisconnectFailsPendingAndEmits(t *testing.T) {
	d, srv, rec := newDriver(t)
	var ext *fakeExt
	ext = startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) {
		_ = ext.ws.Close(websocket.StatusGoingAway, "bye")
		return nil, ""
	})
	waitConnected(t, rec)

	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Eventually(t, func() bool { return rec.has(browser.Event{Type: browser.EventDisconnected}) }, time.Second, 5*time.Millisecond)
}

func TestDriver_NewConnectionReplacesOld(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) { return map[string]any{"windows": []wireWindow{{ID: 1}}}, "" })
	waitConnected(t, rec)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) { return map[string]any{"windows": []wireWindow{{ID: 2}}}, "" })

	assert.Eventually(t, func() bool {
		w, err := d.ListWindows(context.Background())
		return err == nil && len(w) == 1 && w[0].ID == "2"
	}, time.Second, 10*time.Millisecond)
}

func TestTranslateEvent(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	tests := []struct {
		in   message
		want browser.Event
		ok   bool
	}{
		{message{Event: "window.removed", Data: raw(`{"windowId":3}`)}, browser.Event{Type: browser.EventWindowRemoved, WindowID: "3"}, true},
		{message{Event: "tabs.changed", Data: raw(`{"windowId":3}`)}, browser.Event{Type: browser.EventTabsChanged, WindowID: "3"}, true},
		{message{Event: "window.created", Data: raw(`{}`)}, browser.Event{}, false},
		{message{Event: "nope", Data: raw(`{"windowId":3}`)}, browser.Event{}, false},
	}
	for _, tt := range tests {
		got, ok := translateEvent(tt.in)
		assert.Equal(t, tt.ok, ok, tt.in.Event)
		if tt.ok {
			assert.Equal(t, tt.want, got)
		}
	}
}
```

**Step 3: Run the tests to confirm they fail**

Run: `task test --force`
Expected: they FAIL with `undefined: NewDriver`.

**Step 4: Implement `internal/chrome/driver.go`**

```go
package chrome

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/browser"
)

type Driver struct {
	mu      sync.Mutex
	conn    *conn
	events  chan browser.Event
	nextID  atomic.Int64
	timeout time.Duration
}

var (
	_ browser.Driver     = (*Driver)(nil)
	_ browser.HTTPDriver = (*Driver)(nil)
)

type conn struct {
	ws      *websocket.Conn
	mu      sync.Mutex
	pending map[int64]chan message
}

func NewDriver() *Driver {
	return &Driver{events: make(chan browser.Event, 256), timeout: 5 * time.Second}
}

func (d *Driver) Name() string {
	return "chrome"
}

func (d *Driver) Handler() http.Handler {
	return http.HandlerFunc(d.serve)
}

func (d *Driver) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Origin"), "chrome-extension://") {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("chrome: accept websocket failed", "error", err)
		return
	}
	ws.SetReadLimit(4 << 20)
	ctx := r.Context()

	if err := readHello(ctx, ws); err != nil {
		slog.Warn("chrome: extension rejected", "error", err)
		_ = ws.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}

	c := &conn{ws: ws, pending: make(map[int64]chan message)}
	d.swap(c)
	slog.Info("chrome extension connected")
	err = d.readLoop(ctx, c)
	d.drop(c)
	slog.Info("chrome extension disconnected", "reason", err)
}

func readHello(ctx context.Context, ws *websocket.Conn) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		return err
	}
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m.Type != "hello" {
		return fmt.Errorf("expected hello, got %q", m.Type)
	}
	if m.Protocol != ProtocolVersion {
		return fmt.Errorf("extension protocol %d, daemon expects %d; reload the extension", m.Protocol, ProtocolVersion)
	}
	return nil
}

func (d *Driver) swap(c *conn) {
	d.mu.Lock()
	old := d.conn
	d.conn = c
	d.mu.Unlock()
	if old != nil {
		_ = old.ws.Close(websocket.StatusNormalClosure, "replaced")
	}
	d.emit(browser.Event{Type: browser.EventConnected})
}

func (d *Driver) drop(c *conn) {
	d.mu.Lock()
	current := d.conn == c
	if current {
		d.conn = nil
	}
	d.mu.Unlock()
	c.failPending()
	if current {
		d.emit(browser.Event{Type: browser.EventDisconnected})
	}
}

func (d *Driver) readLoop(ctx context.Context, c *conn) error {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return err
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			slog.Warn("chrome: bad frame", "error", err)
			continue
		}
		switch m.Type {
		case "res":
			c.resolve(m)
		case "event":
			if ev, ok := translateEvent(m); ok {
				d.emit(ev)
			}
		}
	}
}

func (d *Driver) emit(ev browser.Event) {
	select {
	case d.events <- ev:
	default:
		slog.Warn("chrome: event buffer full; dropping", "type", ev.Type)
	}
}

func (c *conn) resolve(m message) {
	c.mu.Lock()
	ch := c.pending[m.ID]
	delete(c.pending, m.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (c *conn) failPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = nil
}

func (d *Driver) call(ctx context.Context, method string, params, out any) error {
	d.mu.Lock()
	c := d.conn
	d.mu.Unlock()
	if c == nil {
		return browser.ErrNotConnected
	}

	id := d.nextID.Add(1)
	ch := make(chan message, 1)
	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return browser.ErrNotConnected
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	data, err := json.Marshal(message{Type: "req", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return errors.Join(browser.ErrNotConnected, err)
	}

	select {
	case res, ok := <-ch:
		if !ok {
			return browser.ErrNotConnected
		}
		if res.Error != "" {
			return fmt.Errorf("chrome %s: %s", method, res.Error)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(res.Result, out)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid chrome id %q", id)
	}
	return n, nil
}

func (d *Driver) ListWindows(ctx context.Context) ([]browser.Window, error) {
	var out struct {
		Windows []wireWindow `json:"windows"`
	}
	if err := d.call(ctx, "windows.list", nil, &out); err != nil {
		return nil, err
	}
	windows := make([]browser.Window, len(out.Windows))
	for i, w := range out.Windows {
		windows[i] = w.toWindow()
	}
	return windows, nil
}

func (d *Driver) CreateWindow(ctx context.Context, urls []string, focused bool) (browser.Window, error) {
	var out wireWindow
	if err := d.call(ctx, "windows.create", map[string]any{"urls": urls, "focused": focused}, &out); err != nil {
		return browser.Window{}, err
	}
	return out.toWindow(), nil
}

func (d *Driver) OpenTab(ctx context.Context, windowID, url string) (browser.Tab, error) {
	wid, err := parseID(windowID)
	if err != nil {
		return browser.Tab{}, err
	}
	var out wireTab
	if err := d.call(ctx, "tabs.create", map[string]any{"windowId": wid, "url": url}, &out); err != nil {
		return browser.Tab{}, err
	}
	return out.toTab(), nil
}

func (d *Driver) ActivateTab(ctx context.Context, windowID, tabID string) error {
	wid, err := parseID(windowID)
	if err != nil {
		return err
	}
	tid, err := parseID(tabID)
	if err != nil {
		return err
	}
	return d.call(ctx, "tabs.activate", map[string]any{"windowId": wid, "tabId": tid}, nil)
}

func (d *Driver) FocusWindow(ctx context.Context, windowID string) error {
	wid, err := parseID(windowID)
	if err != nil {
		return err
	}
	return d.call(ctx, "windows.focus", map[string]any{"windowId": wid}, nil)
}

func (d *Driver) Subscribe(ctx context.Context, onEvent func(browser.Event)) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-d.events:
			onEvent(ev)
		}
	}
}
```

Check against the test: `TestDriver_DisconnectFailsPendingAndEmits` closes the socket from inside the request handler. The pending call must come back with `ErrNotConnected`, either through `failPending` closing the channel or through the write error. Both paths are handled.

**Step 5: Run the tests to confirm they pass**

Run: `task test --force`
Expected: they PASS. If `TestDriver_RejectsNonExtensionOrigin` passes only because of the 403, that is the intended path.

**Step 6: Commit**

```bash
git add internal/chrome
git commit -m "feat(chrome): browser.Driver over a websocket to the extension"
```

---

### Task 8: Wire it into the daemon

**Files:**
- Create: `internal/api/browsersessions.go`
- Modify: `internal/api/app.go`
- Test: `internal/api/browsersessions_test.go`

**Step 1: Write the adapter, `internal/api/browsersessions.go`**

```go
package api

import (
	"context"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
)

type browserSessions struct {
	sessions *session.SessionService
}

var _ browser.Sessions = browserSessions{}

func (b browserSessions) IsAttached(ctx context.Context, sessionID uint) (bool, error) {
	s, err := b.sessions.GetSession(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return s.IsAttached, nil
}

func (b browserSessions) Activate(ctx context.Context, sessionID uint) error {
	_, err := b.sessions.ActivateSession(ctx, sessionID)
	return err
}
```

**Step 2: Wire it into `internal/api/app.go`**
- Add the imports `github.com/eleonorayaya/utena/internal/browser` and `github.com/eleonorayaya/utena/internal/chrome`.
- Add the field `Browser *browser.Module` to `App`.
- In `buildApp`, after `sessionModule` is created, build the module and set it on the `App` literal:

```go
browserModule := browser.NewModule(chrome.NewDriver(), browserSessions{sessions: sessionModule.Service}, database)
```

```go
		Browser:   browserModule,
```

- In `modules()`, add `{"browser", "/browser", a.Browser},` directly after the `session` entry, so it starts after sessions and stops before them.

**Step 3: Write a wiring test, `internal/api/browsersessions_test.go`.** It uses the existing `setupTestRouter` from `server_test.go`. That helper builds the app with `mux.FakeDriver` and runs `app.OnStart`. The real `chrome.Driver` it starts is harmless, because nothing connects to it.

```go
package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrowserRoutesMounted(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/browser/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"connected":false}`, w.Body.String())

	body, _ := json.Marshal(map[string]any{"session_id": 1, "url": "https://x.example.com"})
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/browser/open", bytes.NewReader(body)))
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
```

**Step 4: Run the tests and lint**

Run: `task test --force && task lint`
Expected: they PASS.

**Step 5: Commit**

```bash
git add internal/api
git commit -m "feat(api): mount the browser module with the chrome driver"
```

---

### Task 9: The Chrome extension

**Files:**
- Create: `extension/chrome/manifest.json`
- Create: `extension/chrome/background.js`
- Create: `Taskfile.extension.yml`
- Modify: `Taskfile.yml` (add the include, and add to `install`)

**Step 1: `extension/chrome/manifest.json`**

```json
{
  "manifest_version": 3,
  "name": "utena",
  "version": "0.1.0",
  "description": "Lets the utena daemon route links into per-session browser windows.",
  "minimum_chrome_version": "116",
  "permissions": ["tabs", "alarms"],
  "background": { "service_worker": "background.js" }
}
```

**Step 2: `extension/chrome/background.js`**

```js
const PROTOCOL = 1;
const ENDPOINT = "ws://localhost:3333/browser/chrome";
const PING_MS = 20000;
const MAX_BACKOFF_MS = 30000;

let socket = null;
let backoff = 1000;
let pingTimer = null;
let retryTimer = null;

function connect() {
  if (socket && (socket.readyState === WebSocket.OPEN || socket.readyState === WebSocket.CONNECTING)) return;
  clearTimeout(retryTimer);
  const ws = new WebSocket(ENDPOINT);
  socket = ws;
  ws.onopen = () => {
    backoff = 1000;
    send({ type: "hello", protocol: PROTOCOL, browser: "chrome" });
    pingTimer = setInterval(() => send({ type: "ping" }), PING_MS);
  };
  ws.onmessage = (e) => handle(JSON.parse(e.data));
  ws.onclose = () => {
    clearInterval(pingTimer);
    if (socket === ws) socket = null;
    retryTimer = setTimeout(connect, backoff);
    backoff = Math.min(backoff * 2, MAX_BACKOFF_MS);
  };
}

function send(msg) {
  if (socket && socket.readyState === WebSocket.OPEN) socket.send(JSON.stringify(msg));
}

function emit(event, data) {
  send({ type: "event", event, data });
}

function toTab(t) {
  return { id: t.id, url: t.url || t.pendingUrl || "", title: t.title || "", active: !!t.active, pinned: !!t.pinned };
}

function toWindow(w) {
  return { id: w.id, focused: !!w.focused, tabs: (w.tabs || []).map(toTab) };
}

const methods = {
  "windows.list": async () => {
    const wins = await chrome.windows.getAll({ populate: true, windowTypes: ["normal"] });
    return { windows: wins.map(toWindow) };
  },
  "windows.create": async ({ urls, focused }) =>
    toWindow(await chrome.windows.create({ url: urls, focused: !!focused, type: "normal" })),
  "windows.focus": async ({ windowId }) => {
    await chrome.windows.update(windowId, { focused: true });
    return {};
  },
  "tabs.create": async ({ windowId, url }) => toTab(await chrome.tabs.create({ windowId, url, active: true })),
  "tabs.activate": async ({ tabId }) => {
    await chrome.tabs.update(tabId, { active: true });
    return {};
  },
};

async function handle(msg) {
  if (msg.type !== "req") return;
  const fn = methods[msg.method];
  if (!fn) {
    send({ type: "res", id: msg.id, error: `unknown method ${msg.method}` });
    return;
  }
  try {
    send({ type: "res", id: msg.id, result: await fn(msg.params || {}) });
  } catch (err) {
    send({ type: "res", id: msg.id, error: String((err && err.message) || err) });
  }
}

const normalOnly = { windowTypes: ["normal"] };
chrome.windows.onCreated.addListener((w) => emit("window.created", { windowId: w.id }), normalOnly);
chrome.windows.onRemoved.addListener((id) => emit("window.removed", { windowId: id }), normalOnly);
chrome.windows.onFocusChanged.addListener((id) => emit("window.focused", { windowId: id }), normalOnly);

const tabsChanged = (windowId) => emit("tabs.changed", { windowId });
chrome.tabs.onCreated.addListener((t) => tabsChanged(t.windowId));
chrome.tabs.onRemoved.addListener((_, info) => {
  if (!info.isWindowClosing) tabsChanged(info.windowId);
});
chrome.tabs.onUpdated.addListener((_, change, t) => {
  if (change.url || change.title) tabsChanged(t.windowId);
});
chrome.tabs.onActivated.addListener((info) => tabsChanged(info.windowId));
chrome.tabs.onAttached.addListener((_, info) => tabsChanged(info.newWindowId));
chrome.tabs.onDetached.addListener((_, info) => tabsChanged(info.oldWindowId));
chrome.tabs.onMoved.addListener((_, info) => tabsChanged(info.windowId));

chrome.alarms.create("utena-reconnect", { periodInMinutes: 0.5 });
chrome.alarms.onAlarm.addListener((a) => {
  if (a.name === "utena-reconnect") connect();
});
chrome.runtime.onStartup.addListener(connect);
chrome.runtime.onInstalled.addListener(connect);
connect();
```

**Step 3: `Taskfile.extension.yml`**

```yaml
version: '3'

vars:
  EXTENSION_DIR: '{{.HOME}}/.local/share/utena/chrome-extension'

tasks:
  install:
    desc: Copy the Chrome extension to a stable path for "Load unpacked"
    cmds:
      - mkdir -p {{shellQuote .EXTENSION_DIR}}
      - cp extension/chrome/manifest.json extension/chrome/background.js {{shellQuote .EXTENSION_DIR}}/
      - echo "Reload the utena extension in chrome://extensions (first time{{":"}} Developer mode → Load unpacked → {{.EXTENSION_DIR}})"
```

**Step 4: Change `Taskfile.yml`.** Add to `includes:`:

```yaml
  extension:
    taskfile: Taskfile.extension.yml
```

and add `- task: extension:install` to the root `install` task's `cmds`.

**Step 5: Check it**

Run: `task --list-all | grep extension`
Expected: `extension:install` is listed.

**Step 6: Commit**

```bash
git add extension Taskfile.extension.yml Taskfile.yml
git commit -m "feat(extension): chrome MV3 extension speaking the utena browser protocol"
```

---

### Task 10: `utena open`, `utena-open` and `$BROWSER`

**Files:**
- Create: `cmd/tui/open.go`
- Test: `cmd/tui/open_test.go`
- Modify: `cmd/tui/main.go` (register the command, and dispatch on `argv[0]`)
- Modify: `Taskfile.tui.yml` (add the symlink to `install`)
- Modify: `internal/shellinit/shellinit.go`
- Create: `internal/shellinit/shellinit_test.go`

**Step 1: Write the failing tests, `cmd/tui/open_test.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenURL_SendsSessionAndSkipsFallback(t *testing.T) {
	var got browser.OpenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/browser/open", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(browser.OpenResult{WindowID: "1"})
	}))
	defer srv.Close()

	var fellBack []string
	err := openURL(context.Background(), srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, browser.OpenRequest{SessionID: 7, URL: "https://x.example.com"}, got)
	assert.Empty(t, fellBack)
}

func TestOpenURL_FallsBackOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var fellBack []string
	err := openURL(context.Background(), srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example.com"}, fellBack)
}

func TestOpenURL_FallsBackWhenDaemonDown(t *testing.T) {
	var fellBack []string
	err := openURL(context.Background(), "http://127.0.0.1:1", 0, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example.com"}, fellBack)
}

func TestSessionIDFromEnv(t *testing.T) {
	t.Setenv("UTENA_SESSION_ID", "12")
	assert.Equal(t, uint(12), sessionIDFromEnv())
	t.Setenv("UTENA_SESSION_ID", "junk")
	assert.Equal(t, uint(0), sessionIDFromEnv())
	t.Setenv("UTENA_SESSION_ID", "")
	assert.Equal(t, uint(0), sessionIDFromEnv())
}

func TestRewriteArgsForBrowserAlias(t *testing.T) {
	assert.Equal(t, []string{"/x/utena-open", "open", "https://a"}, rewriteArgs([]string{"/x/utena-open", "https://a"}))
	assert.Equal(t, []string{"/x/utena", "top"}, rewriteArgs([]string{"/x/utena", "top"}))
}
```

**Step 2: Run the tests to confirm they fail**

Run: `task test --force`
Expected: they FAIL with `undefined: openURL`, and so on.

**Step 3: Implement `cmd/tui/open.go`**

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/browser"
)

const browserAlias = "utena-open"

func openCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "open <url>...",
		Short:        "Open URLs in this session's browser window",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			base := fmt.Sprintf("http://localhost:%s", port)
			sessionID := sessionIDFromEnv()
			for _, u := range args {
				if err := openURL(cmd.Context(), base, sessionID, u, systemOpen); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func openURL(ctx context.Context, baseURL string, sessionID uint, url string, fallback func(string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := browser.RequestOpen(ctx, http.DefaultClient, baseURL+"/browser", browser.OpenRequest{SessionID: sessionID, URL: url})
	if err != nil {
		return fallback(url)
	}
	return nil
}

func systemOpen(url string) error {
	return exec.Command("/usr/bin/open", url).Run()
}

func sessionIDFromEnv() uint {
	id, err := strconv.ParseUint(os.Getenv("UTENA_SESSION_ID"), 10, 64)
	if err != nil {
		return 0
	}
	return uint(id)
}

func rewriteArgs(args []string) []string {
	if len(args) == 0 || filepath.Base(args[0]) != browserAlias {
		return args
	}
	return append([]string{args[0], "open"}, args[1:]...)
}
```

**Step 4: Wire up `cmd/tui/main.go`.** Add `rootCmd.AddCommand(openCmd())` next to the other `AddCommand` calls. Make `os.Args = rewriteArgs(os.Args)` the first line of `main()`.

**Step 5: Add the symlink in `Taskfile.tui.yml`.** Append to `install.cmds`:

```yaml
      - ln -sf utena {{shellQuote (print (dir .TUI_BIN) "/utena-open")}}
```

**Step 6: Write the failing `shellinit` test, `internal/shellinit/shellinit_test.go`**

```go
package shellinit

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScript_ExportsBrowserInsideTuios(t *testing.T) {
	s := Script()
	idx := strings.Index(s, `export BROWSER="$HOME/.local/bin/utena-open"`)
	assert.Greater(t, idx, strings.Index(s, `if [ -n "$TUIOS_ENV" ]; then`))
	assert.Less(t, idx, strings.LastIndex(s, "fi"))
	assert.Contains(t, s, `[ -x "$HOME/.local/bin/utena-open" ]`)
}
```

**Step 7: Change `internal/shellinit/shellinit.go`.** Inside the `TUIOS_ENV` block, after `unset _utena_val`, add:

```sh
  if [ -x "$HOME/.local/bin/utena-open" ]; then
    export BROWSER="$HOME/.local/bin/utena-open"
  fi
```

**Step 8: Run the tests to confirm they pass**

Run: `task test --force && task lint`
Expected: they PASS.

**Step 9: Commit**

```bash
git add cmd/tui internal/shellinit Taskfile.tui.yml
git commit -m "feat(cli): utena open, utena-open alias and BROWSER in shell-init"
```

---

### Task 11: Route the TUI's `o` through the daemon

**Files:**
- Modify: `internal/tui/provider/sessionsprovider.go` (add an intent)
- Modify: `internal/tui/provider/client.go` (add `openURL`)
- Modify: `internal/tui/sessionlist/sessionlist.go:206,322` (replace the `ui.OpenURL` calls)
- Modify: `internal/tui/sessiondetail/model.go:131,199` (replace the `ui.OpenURL` calls)
- Modify: `internal/tui/ui/prpicker.go` (delete `OpenURL`, keep `OpenURLFailedMsg`)
- Test: `internal/tui/sessionlist/openpr_test.go`, `internal/tui/sessiondetail/interactions_test.go`

**Step 1: Add the intent in `internal/tui/provider/sessionsprovider.go`.** Put it next to `ActivateSession`:

```go
type OpenURLIntentMsg struct {
	SessionID uint
	URL       string
}

func OpenURL(sessionID uint, url string) tea.Cmd {
	return func() tea.Msg { return OpenURLIntentMsg{SessionID: sessionID, URL: url} }
}
```

and in `sessionsProvider.Update`, next to `case activateSessionMsg:`:

```go
	case OpenURLIntentMsg:
		return p, p.client.openURL(msg.SessionID, msg.URL)
```

**Step 2: Add `openURL` to `internal/tui/provider/client.go`.** Import `os/exec`, `context`, `github.com/eleonorayaya/utena/internal/browser` and `github.com/eleonorayaya/utena/internal/tui/ui`. `ui` does not import `provider`, so there's no cycle.

```go
func (c *client) openURL(sessionID uint, url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		req := browser.OpenRequest{SessionID: sessionID, URL: url, Activate: true}
		if _, err := browser.RequestOpen(ctx, c.httpClient, c.baseURL+"/browser", req); err != nil {
			log.Printf("[WARN] browser open via daemon failed, falling back: %v", err)
			cmd := exec.Command("/usr/bin/open", url)
			if err := cmd.Start(); err != nil {
				return ui.OpenURLFailedMsg{Err: err}
			}
			go cmd.Wait()
		}
		return nil
	}
}
```

**Step 3: Update the tests first, so they fail.** In `internal/tui/sessionlist/openpr_test.go`, change `TestOpenPR_SingleOpensDirectly` (and any picker-selection test that only asserts `NotNil(cmd)`) to also check the intent:

```go
	msg := cmd()
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "u1"}, msg)
```

Use the session ID that the test's `withClaude(1, …)` fixture gives the session (check the helper to confirm). For picker selection, find the existing test that selects a PR from the picker and assert the intent carries the **selected session's** ID. Do the same in `internal/tui/sessiondetail/interactions_test.go` for the detail view's `o`.

Run: `task test --force`
Expected: they FAIL, because `cmd()` still returns `nil` from the old `ui.OpenURL`.

**Step 4: Change the call sites**
- `sessiondetail/model.go`: replace both `ui.OpenURL(url)` and `ui.OpenURL(prs[0].HTMLURL)` with `provider.OpenURL(m.sess.ID, …)`.
- `sessionlist/sessionlist.go`: add a field `pickerSessionID uint` to `Model`. Where the picker opens (`m.picker = ui.NewPRPicker(prs)` in the `OpenPR` case), also set `m.pickerSessionID = sel.ID`. Replace the picker-path `ui.OpenURL(url)` with `provider.OpenURL(m.pickerSessionID, url)`, and the single-PR path with `provider.OpenURL(sel.ID, prs[0].HTMLURL)`.
- `ui/prpicker.go`: delete `func OpenURL` and the `os/exec` import if nothing else uses it. Keep `OpenURLFailedMsg`.

**Step 5: Run the tests to confirm they pass**

Run: `task test --force && task lint`
Expected: they PASS.

**Step 6: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): open PRs through the session's browser window"
```

---

### Task 12: Docs and spec amendments

**Files:**
- Modify: `CLAUDE.md` (Component Communication, Go Module Structure, Key Patterns)
- Modify: `README.md` (a new "Browser windows" section under Project Setup)
- Modify: `docs/superpowers/specs/2026-10-15-browser-window-management-design.md`

**Step 1: `CLAUDE.md`**
- Under **Daemon API**, add the line `` - `/browser` - open links in session windows (`POST /open`), driver status, and the `/browser/chrome` extension websocket ``.
- Under **Go Module Structure**, add `` - `internal/browser/` - browser-agnostic domain: session→window ownership, open/dedup/focus rules, `/browser` controller, `Driver` interface `` and `` - `internal/chrome/` - `browser.Driver` over a websocket to the `extension/chrome` MV3 extension ``.
- Under **Key Patterns**, add a **Browser** paragraph that mirrors the Multiplexer one. `browser` owns the domain and talks to browsers only through `browser.Driver`. `internal/chrome` is the production driver (only `internal/api/app.go` imports it). Tests use `browser.FakeDriver`. Events are delivered on the `Subscribe` goroutine, never on the driver's read loop, because `Service.Open` holds its lock across driver calls.

**Step 2: `README.md`.** Add a short **Browser windows** section with these points:
- Run `task extension:install`, then do the one-time Load unpacked step.
- `eval "$(utena shell-init)"` sets `BROWSER` inside tuios.
- `utena open <url>` exists.
- The fallback is `/usr/bin/open`.

**Step 3: Add a "PR 1 amendments" section to the end of the spec.** List the five deviations from this plan's header verbatim.

**Step 4: Commit**

```bash
git add CLAUDE.md README.md docs/superpowers/specs/2026-10-15-browser-window-management-design.md
git commit -m "docs: browser module, chrome driver and extension"
```

---

### Task 13: Manual smoke test and spike notes

This needs a human at the keyboard. The sandbox blocks launching Chrome and running `osascript`.

**Step 1: Install**

```bash
task install
```

Then, in Chrome: open `chrome://extensions`, enable Developer mode, choose **Load unpacked**, and select `~/.local/share/utena/chrome-extension`.

**Step 2: Check the connection**

```bash
curl -s localhost:3333/browser/status
```

Expected: `{"connected":true}`. Also run `task daemon:logs`, which should show `chrome extension connected`.

**Step 3: Smoke checklist.** Run these from a tuios pane in an attached utena session, in a **new** shell so `shell-init` has exported `BROWSER`.
- [ ] `echo $BROWSER` prints `…/utena-open`.
- [ ] `gh pr view --web` (in a repo with a PR) opens a new Chrome window and focuses it.
- [ ] Running it again focuses the same tab and opens no duplicate.
- [ ] `utena open https://example.com` adds a tab to the same window.
- [ ] Run it from a pane of a different, non-attached session, using `UTENA_SESSION_ID=<other> utena open https://example.org`. A window opens without focus.
- [ ] TUI `o` on a non-attached session switches tuios to it, and the PR opens in that session's window.
- [ ] Press cmd-N inside a session window, then `curl -s localhost:3333/browser/status` and check the DB to confirm a new row with the same `session_id`:

  ```bash
  sqlite3 ~/.config/utena/utena.db 'select session_id, state, driver_window_id from browser_windows'
  ```

- [ ] Quit the daemon (`launchctl kickstart -k gui/$(id -u)/com.utena.daemon`). Within about 30 seconds the extension reconnects and ownership survives.
- [ ] Disable the extension. `utena open https://example.com` still opens, through the fallback.

**Step 4: Spike observations for PRs 2 and 3.** Record these in the spec's Risks section.
1. With the extension's service worker console open (`chrome://extensions` → utena → "service worker"), quit Chrome with cmd-Q, then restart it and read the daemon log. Did `window.removed` arrive for each window before the socket closed? Note the gap in milliseconds.
2. Run `aerospace list-windows --all --json --format '%{window-id} %{app-bundle-id} %{window-title}'` while two Chrome windows show the same page. Confirm the duplicate titles.
3. Note where AeroSpace places a window created with `focused:false` (`UTENA_SESSION_ID=<background> utena open …`) and whether it flashes on the current workspace.

**Step 5: Commit the spike notes**

```bash
git add docs/superpowers/specs/2026-10-15-browser-window-management-design.md
git commit -m "docs: record browser spike findings"
```
