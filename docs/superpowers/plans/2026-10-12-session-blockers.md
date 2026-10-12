# Session Blockers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a session carry manual blockers (review / slack / other) and separate blocked sessions in the TUI, CLI and status line.

**Architecture:** A new `SessionBlocker` child table of `Session`, preloaded with the session. "Blocked" is derived via `Session.IsBlocked()`; `Session.Status` is never touched. Three REST endpoints on the session router; CLI and TUI are thin clients over them.

**Tech Stack:** Go, GORM + SQLite, chi, cobra, Bubbletea / Lipgloss / bubbles `textinput`, testify.

**Spec:** `docs/superpowers/specs/2026-10-09-session-blockers-design.md`

## Global Constraints

- Never run `go` directly. Run tests with `task test`, format with `task fmt`, lint with `task lint`, all from the repo root.
- No code comments (repo CLAUDE.md).
- Blocker kinds are exactly `review`, `slack`, `other`. `Note` and `URL` are optional.
- Blocker operations must not modify `Session.Status`.
- Hidden (archived/broken) takes precedence over blocked for grouping.
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. A session whose blockers are all resolved is **not** blocked: it returns to the main group, the CLI shows no `blocked` column, the status line shows it again. (Task 1, Task 4 tests.)
2. An archived session with open blockers stays in the hidden group, not the Blocked section. (Task 4 test.)
3. Resolving/deleting a blocker via another session's ID returns 404 and does not mutate the blocker. (Task 2 test.)
4. Resolving the last open blocker in the detail view must not leave the blocker cursor out of range (no panic, `u`/`x` become no-ops). (Task 6 test.)
5. CLI run outside a utena session with no name argument errors clearly instead of acting on a random session. (Task 3 test.)

---

## File map

| File | Change |
|---|---|
| `internal/session/sessionblocker.go` | Create: `BlockerKind`, `SessionBlocker`, `SessionBlockerStore` |
| `internal/session/sessionblocker_test.go` | Create: model + store tests |
| `internal/session/session.go` | Modify: `Blockers` field, `OpenBlockers`, `IsBlocked` |
| `internal/session/sessionstore.go` | Modify: preload `Blockers` |
| `internal/session/sessionmodule.go` | Modify: register model |
| `internal/session/types.go` | Modify: `IsBlocked` on response, `AddBlockerRequest` |
| `internal/session/sessionstore_test.go`, `sessionsetupstep_test.go`, `sessionservice_pr_test.go` | Modify: add `&SessionBlocker{}` to test migrations |
| `internal/session/sessionservice.go` | Modify: `blockerStore` field, `AddBlocker`, `ResolveBlocker`, `DeleteBlocker` |
| `internal/session/sessioncontroller.go`, `sessionrouter.go` | Modify: three handlers + routes |
| `internal/session/sessionblocker_router_test.go` | Create |
| `cmd/tui/blockers.go`, `cmd/tui/blockers_test.go` | Create: `block`, `blockers`, `unblock` |
| `cmd/tui/sessions.go`, `cmd/tui/statusline.go` (+ tests) | Modify: ls column, status-line exclusion |
| `internal/tui/provider/client.go`, `sessionsprovider.go` | Modify: blocker calls + messages |
| `internal/tui/sessionlist/*` | Modify: grouping, header, tag, `b` key |
| `internal/tui/blockerform/{model.go,keys.go,model_test.go}` | Create |
| `internal/tui/router/view.go`, `internal/tui/app.go` | Modify: `BlockerFormView` |
| `internal/tui/sessiondetail/*` | Modify: blockers section, cursor, `u`/`x`/`b`, refresh |

---

### Task 1: Blocker model, store, and `IsBlocked`

**Files:**
- Create: `internal/session/sessionblocker.go`, `internal/session/sessionblocker_test.go`
- Modify: `internal/session/session.go`, `internal/session/sessionstore.go:24-37`, `internal/session/sessionmodule.go:66-68`, `internal/session/types.go:11-21`
- Modify test migrations: `internal/session/sessionstore_test.go:22,370`, plus every `testdb.New(...)` in `sessionsetupstep_test.go` and `sessionservice_pr_test.go` that lists `&SessionSetupStep{}` — append `&SessionBlocker{}`.

**Interfaces:**
- Produces:
  - `type BlockerKind string`; consts `BlockerKindReview`, `BlockerKindSlack`, `BlockerKindOther`; `var BlockerKinds = []BlockerKind{...}`; `func (k BlockerKind) Valid() bool`
  - `type SessionBlocker struct { gorm.Model; SessionID uint; Kind BlockerKind; Note string; URL string; ResolvedAt *time.Time }` (json: `session_id`, `kind`, `note`, `url`, `resolved_at`)
  - `func (b SessionBlocker) IsOpen() bool`, `func (b SessionBlocker) Label() string`
  - `ErrBlockerNotFound`
  - `NewSessionBlockerStore(db.Database) *SessionBlockerStore` with `Create(*SessionBlocker) error`, `GetByID(uint) (*SessionBlocker, error)`, `Update(*SessionBlocker) error`, `Delete(uint) error`, `ListBySessionID(sessionID uint, includeResolved bool) ([]SessionBlocker, error)`
  - `Session.Blockers []SessionBlocker`, `func (s *Session) OpenBlockers() []SessionBlocker`, `func (s *Session) IsBlocked() bool`
  - `SessionResponse.IsBlocked bool` (json `is_blocked`)

- [ ] **Step 1: Write failing tests** in `internal/session/sessionblocker_test.go`

```go
package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSessionBlocker_Label(t *testing.T) {
	require.Equal(t, "waiting on sam", SessionBlocker{Kind: BlockerKindReview, Note: "waiting on sam", URL: "https://x"}.Label())
	require.Equal(t, "https://x", SessionBlocker{Kind: BlockerKindReview, URL: "https://x"}.Label())
	require.Equal(t, "slack", SessionBlocker{Kind: BlockerKindSlack}.Label())
}

func TestBlockerKind_Valid(t *testing.T) {
	require.True(t, BlockerKindReview.Valid())
	require.True(t, BlockerKindOther.Valid())
	require.False(t, BlockerKind("email").Valid())
	require.False(t, BlockerKind("").Valid())
}

func TestSession_IsBlocked(t *testing.T) {
	now := time.Now()
	s := &Session{}
	require.False(t, s.IsBlocked())

	s.Blockers = []SessionBlocker{{Kind: BlockerKindReview, ResolvedAt: &now}}
	require.False(t, s.IsBlocked())
	require.Empty(t, s.OpenBlockers())

	s.Blockers = append(s.Blockers, SessionBlocker{Model: gorm.Model{ID: 7}, Kind: BlockerKindSlack})
	require.True(t, s.IsBlocked())
	require.Len(t, s.OpenBlockers(), 1)
	require.Equal(t, uint(7), s.OpenBlockers()[0].ID)
}

func TestSessionBlockerStore_CRUD(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, store, swStore, sess, ws1ID)

	bs := NewSessionBlockerStore(store.db)
	open := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindReview, Note: "pr"}
	resolved := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindSlack}
	require.NoError(t, bs.Create(open))
	require.NoError(t, bs.Create(resolved))
	now := time.Now()
	resolved.ResolvedAt = &now
	require.NoError(t, bs.Update(resolved))

	openOnly, err := bs.ListBySessionID(sess.ID, false)
	require.NoError(t, err)
	require.Len(t, openOnly, 1)
	require.Equal(t, open.ID, openOnly[0].ID)

	all, err := bs.ListBySessionID(sess.ID, true)
	require.NoError(t, err)
	require.Len(t, all, 2)

	got, err := bs.GetByID(open.ID)
	require.NoError(t, err)
	require.Equal(t, "pr", got.Note)

	require.NoError(t, bs.Delete(open.ID))
	_, err = bs.GetByID(open.ID)
	require.ErrorIs(t, err, ErrBlockerNotFound)
}

func TestSessionStore_PreloadsBlockers(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, store, swStore, sess, ws1ID)
	require.NoError(t, NewSessionBlockerStore(store.db).Create(&SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.True(t, loaded.IsBlocked())
	require.Equal(t, StatusActive, loaded.Status)

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Len(t, list[0].Blockers, 1)

	require.True(t, NewSessionResponse(loaded).IsBlocked)
}

func TestSessionStore_HardDeleteCascadesBlockers(t *testing.T) {
	store, ws1ID, _ := setupSessionStore(t)
	swStore := NewSessionWorktreeStore(store.db)
	sess := &Session{Name: "s1", Status: StatusArchived}
	addTestSession(t, store, swStore, sess, ws1ID)
	bs := NewSessionBlockerStore(store.db)
	require.NoError(t, bs.Create(&SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}))

	require.NoError(t, store.HardDelete(sess.ID))
	all, err := bs.ListBySessionID(sess.ID, true)
	require.NoError(t, err)
	require.Empty(t, all)
}
```

If `HardDelete` fails on the worktree FK before reaching blockers, drop `TestSessionStore_HardDeleteCascadesBlockers` and note it in the commit message; cascade is declared identically to `SetupSteps`.

- [ ] **Step 2: Run `task test`** — expect compile failure (`undefined: SessionBlocker`).

- [ ] **Step 3: Create `internal/session/sessionblocker.go`**

```go
package session

import (
	"errors"
	"slices"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

var ErrBlockerNotFound = common.NewNotFound("blocker not found")

type BlockerKind string

const (
	BlockerKindReview BlockerKind = "review"
	BlockerKindSlack  BlockerKind = "slack"
	BlockerKindOther  BlockerKind = "other"
)

var BlockerKinds = []BlockerKind{BlockerKindReview, BlockerKindSlack, BlockerKindOther}

func (k BlockerKind) Valid() bool {
	return slices.Contains(BlockerKinds, k)
}

type SessionBlocker struct {
	gorm.Model
	SessionID  uint        `json:"session_id" gorm:"index;not null"`
	Kind       BlockerKind `json:"kind" gorm:"not null"`
	Note       string      `json:"note,omitempty" gorm:"type:text"`
	URL        string      `json:"url,omitempty" gorm:"type:text"`
	ResolvedAt *time.Time  `json:"resolved_at,omitempty"`
}

func (b SessionBlocker) IsOpen() bool {
	return b.ResolvedAt == nil
}

func (b SessionBlocker) Label() string {
	if b.Note != "" {
		return b.Note
	}
	if b.URL != "" {
		return b.URL
	}
	return string(b.Kind)
}

type SessionBlockerStore struct {
	db db.Database
}

func NewSessionBlockerStore(database db.Database) *SessionBlockerStore {
	return &SessionBlockerStore{db: database}
}

func (s *SessionBlockerStore) Create(b *SessionBlocker) error {
	return s.db.Create(b).Error
}

func (s *SessionBlockerStore) GetByID(id uint) (*SessionBlocker, error) {
	var b SessionBlocker
	if err := s.db.First(&b, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrBlockerNotFound
		}
		return nil, err
	}
	return &b, nil
}

func (s *SessionBlockerStore) Update(b *SessionBlocker) error {
	return s.db.Save(b).Error
}

func (s *SessionBlockerStore) Delete(id uint) error {
	return s.db.Unscoped().Delete(&SessionBlocker{}, id).Error
}

func (s *SessionBlockerStore) ListBySessionID(sessionID uint, includeResolved bool) ([]SessionBlocker, error) {
	q := s.db.Where("session_id = ?", sessionID)
	if !includeResolved {
		q = q.Where("resolved_at IS NULL")
	}
	var out []SessionBlocker
	if err := q.Order("created_at ASC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}
```

If `db.Database` lacks `Save`/`Unscoped`, check `internal/db/dbservice.go` for the available methods and use the equivalents (`Model(b).Updates(...)` etc.). `SessionStore.HardDelete` already uses `Unscoped()`.

- [ ] **Step 4: Edit `session.go`** — add the field after `Worktrees`:

```go
	Blockers       []SessionBlocker       `json:"blockers,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
```

and add methods after `IsMulti`:

```go
func (s *Session) OpenBlockers() []SessionBlocker {
	var out []SessionBlocker
	for _, b := range s.Blockers {
		if b.IsOpen() {
			out = append(out, b)
		}
	}
	return out
}

func (s *Session) IsBlocked() bool {
	return len(s.OpenBlockers()) > 0
}
```

- [ ] **Step 5: Edit `sessionstore.go` `loaded()`** — after the `SetupSteps` preload add:

```go
		Preload("Blockers", func(db *gorm.DB) *gorm.DB {
			return db.Order("session_blockers.created_at ASC")
		}).
```

- [ ] **Step 6: Edit `sessionmodule.go` `Models()`** to append `&SessionBlocker{}`.

- [ ] **Step 7: Edit `types.go`**:

```go
type SessionResponse struct {
	*Session
	AttentionStatus claude.ClaudeSessionStatus `json:"attention_status,omitempty"`
	IsBlocked       bool                       `json:"is_blocked"`
}

func NewSessionResponse(session *Session) *SessionResponse {
	return &SessionResponse{
		Session:         session,
		AttentionStatus: session.AttentionStatus(),
		IsBlocked:       session.IsBlocked(),
	}
}
```

- [ ] **Step 8: Add `&SessionBlocker{}`** to every `testdb.New(...)` call in `internal/session/*_test.go` that includes `&SessionSetupStep{}` (`rg -n "SessionSetupStep\{\}\)" internal/session`).

- [ ] **Step 9: Run `task test`** — all pass.

- [ ] **Step 10: Commit**

```bash
git add internal/session
git commit -m "feat(session): add SessionBlocker model and IsBlocked"
```

---

### Task 2: Service methods and REST endpoints

**Files:**
- Modify: `internal/session/sessionservice.go:84-100` (struct + constructor), add methods at end of file
- Modify: `internal/session/types.go` (add `AddBlockerRequest`)
- Modify: `internal/session/sessioncontroller.go` (three handlers), `internal/session/sessionrouter.go`
- Create: `internal/session/sessionblocker_router_test.go`

**Interfaces:**
- Consumes: Task 1 types/store.
- Produces:
  - `type AddBlockerInput struct { Kind BlockerKind; Note string; URL string }`
  - `func (s *SessionService) AddBlocker(ctx context.Context, sessionID uint, in AddBlockerInput) (*Session, error)`
  - `func (s *SessionService) ResolveBlocker(ctx context.Context, sessionID, blockerID uint) (*Session, error)`
  - `func (s *SessionService) DeleteBlocker(ctx context.Context, sessionID, blockerID uint) error`
  - `type AddBlockerRequest struct { Kind BlockerKind \`json:"kind"\`; Note string \`json:"note,omitempty"\`; URL string \`json:"url,omitempty"\` }`
  - Routes: `POST /{id}/blockers` → 201 `SessionResponse`; `PUT /{id}/blockers/{blockerId}/resolve` → 200 `SessionResponse`; `DELETE /{id}/blockers/{blockerId}` → 204.

The service builds its own blocker store from the session store's DB (`NewSessionBlockerStore(store.db)`) so the 11 existing `NewSessionService(...)` call sites stay unchanged.

- [ ] **Step 1: Write failing router tests** in `internal/session/sessionblocker_router_test.go`

```go
package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func doJSON(t *testing.T, router *SessionRouter, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	return w
}

func TestSessionRouter_AddBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)

	w := doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "review", "url": "https://github.com/o/r/pull/1"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.IsBlocked)
	require.Len(t, resp.Blockers, 1)
	require.Equal(t, BlockerKindReview, resp.Blockers[0].Kind)
	require.Equal(t, StatusActive, resp.Status)
}

func TestSessionRouter_AddBlocker_InvalidKind(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)

	w := doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "email"})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_AddBlocker_UnknownOrDeletedSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	w := doJSON(t, router, "POST", "/9999/blockers", map[string]string{"kind": "other"})
	require.Equal(t, http.StatusNotFound, w.Code)

	sess := &Session{Name: "gone", Status: StatusDeleted}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	w = doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "other"})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_ResolveBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusInactive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindSlack}
	require.NoError(t, bs.Create(b))

	path := fmt.Sprintf("/%d/blockers/%d/resolve", sess.ID, b.ID)
	w := doJSON(t, router, "PUT", path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.False(t, resp.IsBlocked)
	require.Equal(t, StatusInactive, resp.Status)

	first, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.NotNil(t, first.ResolvedAt)

	w = doJSON(t, router, "PUT", path, nil)
	require.Equal(t, http.StatusOK, w.Code)
	again, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.True(t, first.ResolvedAt.Equal(*again.ResolvedAt))
}

func TestSessionRouter_BlockerOwnership(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)
	a := &Session{Name: "a", Status: StatusActive}
	other := &Session{Name: "b", Status: StatusActive}
	addTestSession(t, sessionStore, swStore, a, ws1ID)
	addTestSession(t, sessionStore, swStore, other, ws2ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: a.ID, Kind: BlockerKindOther}
	require.NoError(t, bs.Create(b))

	w := doJSON(t, router, "PUT", fmt.Sprintf("/%d/blockers/%d/resolve", other.ID, b.ID), nil)
	require.Equal(t, http.StatusNotFound, w.Code)
	w = doJSON(t, router, "DELETE", fmt.Sprintf("/%d/blockers/%d", other.ID, b.ID), nil)
	require.Equal(t, http.StatusNotFound, w.Code)

	still, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.Nil(t, still.ResolvedAt)
}

func TestSessionRouter_DeleteBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}
	require.NoError(t, bs.Create(b))

	w := doJSON(t, router, "DELETE", fmt.Sprintf("/%d/blockers/%d", sess.ID, b.ID), nil)
	require.Equal(t, http.StatusNoContent, w.Code)
	_, err := bs.GetByID(b.ID)
	require.ErrorIs(t, err, ErrBlockerNotFound)
}
```

- [ ] **Step 2: Run `task test`** — expect failures (404s / compile errors on `AddBlockerRequest`).

- [ ] **Step 3: Add to `types.go`**

```go
type AddBlockerRequest struct {
	Kind BlockerKind `json:"kind"`
	Note string      `json:"note,omitempty"`
	URL  string      `json:"url,omitempty"`
}

func (r *AddBlockerRequest) Bind(_ *http.Request) error {
	if !r.Kind.Valid() {
		return fmt.Errorf("kind must be one of review, slack, other (got %q)", r.Kind)
	}
	return nil
}
```

- [ ] **Step 4: Service** — add field `blockerStore *SessionBlockerStore` to `SessionService`, set `blockerStore: NewSessionBlockerStore(store.db),` in `NewSessionService`. Append to `sessionservice.go`:

```go
type AddBlockerInput struct {
	Kind BlockerKind
	Note string
	URL  string
}

func (s *SessionService) liveSession(id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status == StatusDeleted {
		return nil, ErrSessionNotFound
	}
	return sess, nil
}

func (s *SessionService) ownedBlocker(sessionID, blockerID uint) (*SessionBlocker, error) {
	if _, err := s.liveSession(sessionID); err != nil {
		return nil, err
	}
	b, err := s.blockerStore.GetByID(blockerID)
	if err != nil {
		return nil, err
	}
	if b.SessionID != sessionID {
		return nil, ErrBlockerNotFound
	}
	return b, nil
}

func (s *SessionService) AddBlocker(_ context.Context, sessionID uint, in AddBlockerInput) (*Session, error) {
	if _, err := s.liveSession(sessionID); err != nil {
		return nil, err
	}
	b := &SessionBlocker{SessionID: sessionID, Kind: in.Kind, Note: in.Note, URL: in.URL}
	if err := s.blockerStore.Create(b); err != nil {
		return nil, fmt.Errorf("failed to add blocker: %w", err)
	}
	return s.store.GetByID(sessionID)
}

func (s *SessionService) ResolveBlocker(_ context.Context, sessionID, blockerID uint) (*Session, error) {
	b, err := s.ownedBlocker(sessionID, blockerID)
	if err != nil {
		return nil, err
	}
	if b.IsOpen() {
		now := time.Now()
		b.ResolvedAt = &now
		if err := s.blockerStore.Update(b); err != nil {
			return nil, fmt.Errorf("failed to resolve blocker: %w", err)
		}
	}
	return s.store.GetByID(sessionID)
}

func (s *SessionService) DeleteBlocker(_ context.Context, sessionID, blockerID uint) error {
	if _, err := s.ownedBlocker(sessionID, blockerID); err != nil {
		return err
	}
	return s.blockerStore.Delete(blockerID)
}
```

- [ ] **Step 5: Controller** — append to `sessioncontroller.go`:

```go
func (c *SessionController) AddBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	data := &AddBlockerRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.AddBlocker(ctx, id, AddBlockerInput{Kind: data.Kind, Note: data.Note, URL: data.URL})
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	render.Status(r, http.StatusCreated)
	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) ResolveBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}
	blockerID, err := parseUintParam(r, "blockerId")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	session, err := c.service.ResolveBlocker(ctx, id, blockerID)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}

	common.RenderResponse(w, r, NewSessionResponse(session))
}

func (c *SessionController) DeleteBlocker(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := parseUintParam(r, "id")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}
	blockerID, err := parseUintParam(r, "blockerId")
	if err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.DeleteBlocker(ctx, id, blockerID); err != nil {
		common.RenderError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 6: Router** — in `sessionrouter.go` before `r.Get("/{id}", ...)`:

```go
	r.Post("/{id}/blockers", sr.controller.AddBlocker)
	r.Put("/{id}/blockers/{blockerId}/resolve", sr.controller.ResolveBlocker)
	r.Delete("/{id}/blockers/{blockerId}", sr.controller.DeleteBlocker)
```

- [ ] **Step 7: Run `task test`** — all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/session
git commit -m "feat(session): add blocker endpoints"
```

---

### Task 3: CLI — `block`, `blockers`, `unblock`, ls column, status line

**Files:**
- Create: `cmd/tui/blockers.go`, `cmd/tui/blockers_test.go`
- Modify: `cmd/tui/sessions.go` (register subcommands, ls column), `cmd/tui/statusline.go` (skip blocked), `cmd/tui/statusline_test.go`

**Interfaces:**
- Consumes: `session.SessionResponse.IsBlocked`, `Session.OpenBlockers()`, `SessionBlocker.Label()`, `fetchSessions(port)`, `fetchSessionListResponse`.
- Produces: `func resolveSession(sessions []session.Session, name string, envID string) (*session.Session, error)`; `type barRow struct { Name string; Attention claude.ClaudeSessionStatus; Blocked bool }`.

- [ ] **Step 1: Write failing tests** in `cmd/tui/blockers_test.go`

```go
package main

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"gorm.io/gorm"
)

func TestResolveSession(t *testing.T) {
	sessions := []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "alpha"},
		{Model: gorm.Model{ID: 2}, Name: "beta"},
	}

	s, err := resolveSession(sessions, "beta", "1")
	if err != nil || s.ID != 2 {
		t.Fatalf("by name: got %v, %v", s, err)
	}

	s, err = resolveSession(sessions, "", "1")
	if err != nil || s.ID != 1 {
		t.Fatalf("by env: got %v, %v", s, err)
	}

	if _, err := resolveSession(sessions, "", ""); err == nil {
		t.Fatal("expected error with no name and no UTENA_SESSION_ID")
	}
	if _, err := resolveSession(sessions, "nope", ""); err == nil {
		t.Fatal("expected error for unknown name")
	}
	if _, err := resolveSession(sessions, "", "99"); err == nil {
		t.Fatal("expected error for unknown env id")
	}
}
```

Append to `cmd/tui/statusline_test.go`:

```go
func TestFormatStatusLine_SkipsBlocked(t *testing.T) {
	rows := []barRow{
		{Name: "a", Attention: claude.StatusNeedsAttention},
		{Name: "b", Attention: claude.StatusNeedsAttention, Blocked: true},
	}
	if got := formatStatusLine(rows); got != "! a" {
		t.Fatalf("got %q", got)
	}
}
```

(Add the `claude` import if the file lacks it.)

- [ ] **Step 2: Run `task test`** — expect compile failure.

- [ ] **Step 3: Create `cmd/tui/blockers.go`**

```go
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
)

func resolveSession(sessions []session.Session, name string, envID string) (*session.Session, error) {
	if name != "" {
		for i := range sessions {
			if sessions[i].Name == name {
				return &sessions[i], nil
			}
		}
		return nil, fmt.Errorf("no session named %q", name)
	}
	if envID == "" {
		return nil, errors.New("no session name given and UTENA_SESSION_ID is not set")
	}
	id, err := strconv.ParseUint(envID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid UTENA_SESSION_ID %q", envID)
	}
	for i := range sessions {
		if sessions[i].ID == uint(id) {
			return &sessions[i], nil
		}
	}
	return nil, fmt.Errorf("no session with id %d", id)
}

func lookupSession(port, name string) (*session.Session, error) {
	sessions, err := fetchSessions(port)
	if err != nil {
		return nil, err
	}
	return resolveSession(sessions, name, os.Getenv("UTENA_SESSION_ID"))
}

func blockerRequest(method, url string, body any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, url, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(res.Body)
		return fmt.Errorf("%s %s: status %d: %s", method, url, res.StatusCode, bytes.TrimSpace(msg))
	}
	return nil
}

func optionalName(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func sessionsBlockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "block [name]",
		Short:        "Add a blocker to a session",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			kind, _ := cmd.Flags().GetString("kind")
			note, _ := cmd.Flags().GetString("note")
			url, _ := cmd.Flags().GetString("url")
			if !session.BlockerKind(kind).Valid() {
				return fmt.Errorf("--kind must be one of review, slack, other")
			}
			sess, err := lookupSession(port, optionalName(args))
			if err != nil {
				return err
			}
			return blockerRequest(http.MethodPost,
				fmt.Sprintf("http://localhost:%s/sessions/%d/blockers", port, sess.ID),
				session.AddBlockerRequest{Kind: session.BlockerKind(kind), Note: note, URL: url})
		},
	}
	cmd.Flags().String("kind", "", "review, slack or other")
	cmd.Flags().String("note", "", "what you're waiting on")
	cmd.Flags().String("url", "", "link to the PR or Slack message")
	_ = cmd.MarkFlagRequired("kind")
	return cmd
}

func sessionsBlockersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "blockers [name]",
		Short:        "List a session's blockers (id, kind, label, age)",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")
			sess, err := lookupSession(port, optionalName(args))
			if err != nil {
				return err
			}
			for _, b := range sess.Blockers {
				if !all && !b.IsOpen() {
					continue
				}
				line := fmt.Sprintf("%d\t%s\t%s\t%s", b.ID, b.Kind, b.Label(), common.TimeAgo(b.CreatedAt))
				if !b.IsOpen() {
					line += "\tresolved"
				}
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "include resolved blockers")
	return cmd
}

func sessionsUnblockCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "unblock [name] [blocker-id]",
		Short:        "Resolve a blocker (or all with --all)",
		Args:         cobra.MaximumNArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			all, _ := cmd.Flags().GetBool("all")

			var name, idArg string
			switch {
			case all && len(args) == 2:
				return errors.New("--all takes at most a session name")
			case all:
				name = optionalName(args)
			case len(args) == 2:
				name, idArg = args[0], args[1]
			case len(args) == 1:
				idArg = args[0]
			default:
				return errors.New("blocker id required (or pass --all)")
			}

			sess, err := lookupSession(port, name)
			if err != nil {
				return err
			}

			var ids []uint
			if all {
				for _, b := range sess.OpenBlockers() {
					ids = append(ids, b.ID)
				}
			} else {
				id, err := strconv.ParseUint(idArg, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid blocker id %q", idArg)
				}
				ids = []uint{uint(id)}
			}

			for _, id := range ids {
				if err := blockerRequest(http.MethodPut,
					fmt.Sprintf("http://localhost:%s/sessions/%d/blockers/%d/resolve", port, sess.ID, id), nil); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("all", false, "resolve every open blocker")
	return cmd
}
```

- [ ] **Step 4: Edit `sessions.go`** — register subcommands:

```go
	cmd.AddCommand(sessionsLsCmd())
	cmd.AddCommand(sessionsBlockCmd())
	cmd.AddCommand(sessionsBlockersCmd())
	cmd.AddCommand(sessionsUnblockCmd())
```

and change the ls print line to:

```go
			for _, s := range filterSessions(sessions, all) {
				blocked := ""
				if s.IsBlocked() {
					blocked = "blocked"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", s.Name, s.Status, s.SessionRoot, blocked)
			}
```

- [ ] **Step 5: Edit `statusline.go`** — add `Blocked bool` to `barRow`, set `Blocked: sr.IsBlocked` when building rows, and at the top of the loop in `formatStatusLine`:

```go
	for _, r := range rows {
		if r.Blocked {
			continue
		}
```

- [ ] **Step 6: Run `task test`** — all pass.

- [ ] **Step 7: Commit**

```bash
git add cmd/tui
git commit -m "feat(cli): add sessions block/blockers/unblock"
```

---

### Task 4: TUI provider calls and session list grouping

**Files:**
- Modify: `internal/tui/provider/client.go`, `internal/tui/provider/sessionsprovider.go`
- Modify: `internal/tui/sessionlist/sessionlist.go`, `internal/tui/sessionlist/keys.go`, `internal/tui/sessionlist/sessionlist_test.go`

**Interfaces:**
- Consumes: `Session.IsBlocked()`, `Session.OpenBlockers()`, `session.BlockerKind`.
- Produces (provider):
  - `func AddBlocker(sessionID uint, kind session.BlockerKind, note, url string) tea.Cmd`
  - `func ResolveBlocker(sessionID, blockerID uint) tea.Cmd`
  - `func DeleteBlocker(sessionID, blockerID uint) tea.Cmd`
  - `type BlockerAddedMsg struct { SessionID uint }` (emitted after a successful add, alongside a sessions refetch)
- Produces (router, used by Task 5/6): `router.BlockerFormView` constant (add here so the `b` key compiles); `blockerform.Start` is wired in Task 5 — in this task the `b` key only navigates.

- [ ] **Step 1: Write failing tests** — append to `sessionlist_test.go`:

```go
func openBlocker(kind session.BlockerKind) session.SessionBlocker {
	return session.SessionBlocker{Kind: kind}
}

func TestSessionList_GroupsBlockedBetweenActiveAndHidden(t *testing.T) {
	m := New()
	m.showHidden = true
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "blocked1", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindReview)}},
		{Model: gorm.Model{ID: 2}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 3}, Name: "old", Status: session.StatusArchived, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindSlack)}},
		{Model: gorm.Model{ID: 4}, Name: "live2", Status: session.StatusInactive},
	}
	m.rebuildFiltered()

	var names []string
	for _, s := range m.filtered {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"live", "live2", "blocked1", "old"}, names)
}

func TestSessionList_ResolvedBlockersAreNotBlocked(t *testing.T) {
	now := time.Now()
	m := New()
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "was-blocked", Status: session.StatusActive, Blockers: []session.SessionBlocker{{Kind: session.BlockerKindReview, ResolvedAt: &now}}},
		{Model: gorm.Model{ID: 2}, Name: "live", Status: session.StatusActive},
	}
	m.rebuildFiltered()
	assert.Equal(t, "was-blocked", m.filtered[0].Name)
	assert.NotContains(t, m.renderList(60), "Blocked")
}

func TestSessionList_RendersBlockedHeaderAndTag(t *testing.T) {
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "waiting", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindSlack), openBlocker(session.BlockerKindReview)}},
	}
	m.rebuildFiltered()
	out := m.renderList(60)
	assert.Contains(t, out, "Blocked")
	assert.Contains(t, out, "⏸ slack ×2")
	assert.Less(t, strings.Index(out, "live"), strings.Index(out, "Blocked"))
	assert.Less(t, strings.Index(out, "Blocked"), strings.Index(out, "waiting"))
}

func TestSessionList_CursorMovesAcrossHeader(t *testing.T) {
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "waiting", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindOther)}},
	}
	m.rebuildFiltered()
	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, "waiting", m.selectedSession().Name)
}

func TestSessionList_BlockKey_NavigatesToBlockerForm(t *testing.T) {
	m := New()
	m.filtered = []session.Session{{Model: gorm.Model{ID: 5}, Name: "s", Status: session.StatusActive}}
	_, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	assert.True(t, handled)
	assert.NotNil(t, cmd)
}
```

Add `"strings"` and `"time"` to the test imports.

- [ ] **Step 2: Run `task test`** — expect failures.

- [ ] **Step 3: Provider** — in `sessionsprovider.go` add:

```go
func AddBlocker(sessionID uint, kind session.BlockerKind, note, url string) tea.Cmd {
	return func() tea.Msg {
		return addBlockerIntentMsg{sessionID: sessionID, req: session.AddBlockerRequest{Kind: kind, Note: note, URL: url}}
	}
}

func ResolveBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg { return resolveBlockerIntentMsg{sessionID: sessionID, blockerID: blockerID} }
}

func DeleteBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg { return deleteBlockerIntentMsg{sessionID: sessionID, blockerID: blockerID} }
}

type addBlockerIntentMsg struct {
	sessionID uint
	req       session.AddBlockerRequest
}

type resolveBlockerIntentMsg struct{ sessionID, blockerID uint }

type deleteBlockerIntentMsg struct{ sessionID, blockerID uint }

type blockerAddedMsg struct{ sessionID uint }

type blockerChangedMsg struct{}

type BlockerAddedMsg struct {
	SessionID uint
}
```

and in `sessionsProvider.Update`:

```go
	case addBlockerIntentMsg:
		return p, p.client.addBlocker(msg.sessionID, msg.req)

	case blockerAddedMsg:
		id := msg.sessionID
		return p, tea.Batch(
			p.client.fetchSessions(),
			func() tea.Msg { return BlockerAddedMsg{SessionID: id} },
		)

	case resolveBlockerIntentMsg:
		return p, p.client.resolveBlocker(msg.sessionID, msg.blockerID)

	case deleteBlockerIntentMsg:
		return p, p.client.deleteBlocker(msg.sessionID, msg.blockerID)

	case blockerChangedMsg:
		return p, p.client.fetchSessions()
```

In `client.go` add:

```go
func (c *client) addBlocker(sessionID uint, reqBody session.AddBlockerRequest) tea.Cmd {
	return func() tea.Msg {
		body, err := json.Marshal(reqBody)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Post(fmt.Sprintf("%s/sessions/%d/blockers", c.baseURL, sessionID), "application/json", bytes.NewReader(body))
		if err != nil {
			log.Printf("[ERROR] add blocker %d: %v", sessionID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusCreated {
			return parseAPIError(res, "add blocker")
		}
		return blockerAddedMsg{sessionID: sessionID}
	}
}

func (c *client) resolveBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("%s/sessions/%d/blockers/%d/resolve", c.baseURL, sessionID, blockerID), nil)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] resolve blocker %d: %v", blockerID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			return parseAPIError(res, "resolve blocker")
		}
		return blockerChangedMsg{}
	}
}

func (c *client) deleteBlocker(sessionID, blockerID uint) tea.Cmd {
	return func() tea.Msg {
		req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/sessions/%d/blockers/%d", c.baseURL, sessionID, blockerID), nil)
		if err != nil {
			return ErrMsg{err}
		}
		res, err := c.httpClient.Do(req)
		if err != nil {
			log.Printf("[ERROR] delete blocker %d: %v", blockerID, err)
			return ErrMsg{err}
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusNoContent {
			return parseAPIError(res, "delete blocker")
		}
		return blockerChangedMsg{}
	}
}
```

- [ ] **Step 4: Router view** — append `BlockerFormView` to the `View` const block in `internal/tui/router/view.go`.

- [ ] **Step 5: Session list grouping** — replace the sort in `rebuildFiltered`:

```go
	sort.SliceStable(m.filtered, func(i, j int) bool {
		return listGroup(m.filtered[i]) < listGroup(m.filtered[j])
	})
```

and add:

```go
func listGroup(s session.Session) int {
	switch {
	case s.IsHidden():
		return 2
	case s.IsBlocked():
		return 1
	default:
		return 0
	}
}

func (m Model) firstBlockedIndex() int {
	for i, s := range m.filtered {
		if listGroup(s) == 1 {
			return i
		}
	}
	return -1
}
```

Change `bodyHeight` so the header's two lines are reserved when it is present:

```go
func (m Model) bodyHeight() int {
	h := m.height - 3
	if m.firstBlockedIndex() >= 0 {
		h -= 2
	}
	if h < 1 {
		return 1
	}
	return h
}
```

In `renderList`, extract the existing title + rule into a helper and draw it again before the first blocked row:

```go
func sectionHeader(title string, width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
	return titleStyle.Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant).Render(strings.Repeat("─", width))
}
```

`renderList` body:

```go
	var b strings.Builder
	b.WriteString(sectionHeader("Sessions", width))
	b.WriteString("\n")

	bh := m.bodyHeight()
	end := min(m.offset+bh, len(m.filtered))
	visible := m.filtered
	if m.offset < len(visible) {
		visible = visible[m.offset:end]
	} else {
		visible = nil
	}

	blockedAt := m.firstBlockedIndex()
	for i, s := range visible {
		idx := i + m.offset
		if idx == blockedAt {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(sectionHeader("Blocked", width))
			b.WriteString("\n")
		}
		b.WriteString(m.renderRow(s, idx == m.cursor, width))
		if i < len(visible)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
```

In `renderRow`, replace the badge computation:

```go
	var badgeStr string
	if s.IsBlocked() && !s.IsHidden() {
		badgeStr = lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(" " + blockedTag(s) + " ")
	} else {
		badgeStr = statusview.NewStatusBadge(s.ClaudeSessions, false).View()
	}
	badgeW := lipgloss.Width(badgeStr)
```

and add:

```go
func blockedTag(s session.Session) string {
	open := s.OpenBlockers()
	tag := "⏸ " + string(open[0].Kind)
	if len(open) > 1 {
		tag += fmt.Sprintf(" ×%d", len(open))
	}
	return tag
}
```

- [ ] **Step 6: `b` key** — in `keys.go` add `Block key.Binding` to `keyMap`, include it in `ShortHelp`/`FullHelp` after `Delete`, and:

```go
	Block:        key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
```

In `OnKeyMsg` add a case:

```go
	case key.Matches(msg, keys.Block):
		if m.selectedSession() == nil {
			return m, nil, false
		}
		return m, router.NavigateTo(router.BlockerFormView), true
```

(Task 5 changes this to also send `blockerform.Start`.)

- [ ] **Step 7: Run `task test`** — all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/provider internal/tui/sessionlist internal/tui/router
git commit -m "feat(tui): group blocked sessions in the session list"
```

---

### Task 5: Blocker form view

**Files:**
- Create: `internal/tui/blockerform/model.go`, `internal/tui/blockerform/keys.go`, `internal/tui/blockerform/model_test.go`
- Modify: `internal/tui/app.go` (register view), `internal/tui/sessionlist/sessionlist.go` (`b` sends `Start`)

**Interfaces:**
- Consumes: `provider.AddBlocker`, `provider.BlockerAddedMsg`, `provider.ErrMsg`, `router.BlockerFormView`, `router.Back()`, `session.BlockerKinds`.
- Produces: `blockerform.New() Model`, `blockerform.Start(sessionID uint) tea.Cmd`, `blockerform.StartMsg{SessionID uint}`.

- [ ] **Step 1: Write failing tests** in `internal/tui/blockerform/model_test.go`

```go
package blockerform

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func started(t *testing.T) Model {
	t.Helper()
	m, _ := New().Init()
	m, _ = m.Update(StartMsg{SessionID: 4})
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestBlockerForm_CyclesKind(t *testing.T) {
	m := started(t)
	assert.Equal(t, session.BlockerKindReview, m.kind())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, session.BlockerKindSlack, m.kind())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, session.BlockerKindOther, m.kind())
}

func TestBlockerForm_SubmitSendsAddBlocker(t *testing.T) {
	m := started(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "waiting on sam")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.addBlockerIntentMsg", reflect.TypeOf(cmd()).String())
}

func TestBlockerForm_SubmitWithEverythingEmptyIsAllowed(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
}

func TestBlockerForm_EscGoesBack(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	assert.IsType(t, router.BackMsg{}, cmd())
}

func TestBlockerForm_AddedGoesBack(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(provider.BlockerAddedMsg{SessionID: 4})
	require.NotNil(t, cmd)
	assert.IsType(t, router.BackMsg{}, cmd())
}

func TestBlockerForm_ArrowsInTextFieldDoNotChangeKind(t *testing.T) {
	m := started(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, session.BlockerKindReview, m.kind())
}
```

If `router.BackMsg` is not the exported name, check `internal/tui/router/messages.go` (the sessiondetail test already uses `router.BackMsg{}`).

- [ ] **Step 2: Run `task test`** — expect compile failure.

- [ ] **Step 3: Create `keys.go`**

```go
package blockerform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
)

type keyMap struct {
	Kind   key.Binding
	Next   key.Binding
	Prev   key.Binding
	Submit key.Binding
	Back   key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Kind, k.Next, k.Submit, k.Back}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Kind, k.Next, k.Prev, k.Submit, k.Back}}
}

var keys = keyMap{
	Kind:   key.NewBinding(key.WithKeys("left", "right"), key.WithHelp("←/→", "kind")),
	Next:   key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	Prev:   key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev field")),
	Submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "save")),
	Back:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

var _ help.KeyMap = keys
```

- [ ] **Step 4: Create `model.go`**

```go
package blockerform

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

const (
	kindField = iota
	noteField
	urlField
	fieldCount
)

type Model struct {
	sessionID uint
	kindIdx   int
	focus     int
	note      textinput.Model
	url       textinput.Model
	err       string
	width     int
	height    int
}

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func New() Model {
	note := textinput.New()
	note.Prompt = "Note: "
	note.Placeholder = "optional — what are you waiting on?"

	url := textinput.New()
	url.Prompt = "URL:  "
	url.Placeholder = "optional — PR or Slack link"

	return Model{note: note, url: url}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.sessionID = 0
	m.reset()
	return m, nil
}

func (m *Model) reset() {
	m.kindIdx = 0
	m.focus = kindField
	m.err = ""
	m.note.SetValue("")
	m.url.SetValue("")
	m.note.Blur()
	m.url.Blur()
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) kind() session.BlockerKind {
	return session.BlockerKinds[m.kindIdx]
}

func (m *Model) setFocus(f int) {
	m.focus = (f + fieldCount) % fieldCount
	m.note.Blur()
	m.url.Blur()
	switch m.focus {
	case noteField:
		m.note.Focus()
	case urlField:
		m.url.Focus()
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case StartMsg:
		m.sessionID = msg.SessionID
		m.reset()
		return m, nil
	case provider.BlockerAddedMsg:
		return m, router.Back()
	case provider.ErrMsg:
		m.err = msg.Err.Error()
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		return m, router.Back()
	case key.Matches(msg, keys.Submit):
		if m.sessionID == 0 {
			return m, nil
		}
		return m, provider.AddBlocker(m.sessionID, m.kind(),
			strings.TrimSpace(m.note.Value()), strings.TrimSpace(m.url.Value()))
	case key.Matches(msg, keys.Next):
		m.setFocus(m.focus + 1)
		return m, nil
	case key.Matches(msg, keys.Prev):
		m.setFocus(m.focus - 1)
		return m, nil
	}

	if m.focus == kindField {
		n := len(session.BlockerKinds)
		switch msg.Type {
		case tea.KeyRight:
			m.kindIdx = (m.kindIdx + 1) % n
		case tea.KeyLeft:
			m.kindIdx = (m.kindIdx - 1 + n) % n
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.focus == noteField {
		m.note, cmd = m.note.Update(msg)
	} else {
		m.url, cmd = m.url.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("Add blocker"))
	b.WriteString("\n\n")

	var kinds []string
	for i, k := range session.BlockerKinds {
		style := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		if i == m.kindIdx {
			style = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
			if m.focus == kindField {
				style = style.Underline(true)
			}
		}
		kinds = append(kinds, style.Render(string(k)))
	}
	b.WriteString("Kind: " + strings.Join(kinds, "  ") + "\n")
	b.WriteString(m.note.View() + "\n")
	b.WriteString(m.url.View() + "\n")

	if m.err != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(theme.Current.Error).Render(m.err) + "\n")
	}
	return b.String()
}
```

Note: the `kind()` test with Left-Left from Slack → Review → Other relies on wrap-around; the test above starts at Review, goes Right (Slack), Left (Review), Left (Other).

- [ ] **Step 5: Register view** — in `internal/tui/app.go` import `blockerform` and add:

```go
		router.BlockerFormView:        &router.ViewAdapter[blockerform.Model]{Model: blockerform.New()},
```

- [ ] **Step 6: Session list `b` key** — change the case added in Task 4 to:

```go
	case key.Matches(msg, keys.Block):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		return m, tea.Sequence(
			router.NavigateTo(router.BlockerFormView),
			blockerform.Start(sel.ID),
		), true
```

(import `github.com/eleonorayaya/utena/internal/tui/blockerform`).

- [ ] **Step 7: Run `task test`** — all pass.

- [ ] **Step 8: Commit**

```bash
git add internal/tui
git commit -m "feat(tui): add blocker form"
```

---

### Task 6: Session detail — blockers section, resolve, delete

**Files:**
- Modify: `internal/tui/sessiondetail/model.go`, `internal/tui/sessiondetail/keys.go`, `internal/tui/sessiondetail/model_test.go`

**Interfaces:**
- Consumes: `provider.ResolveBlocker`, `provider.DeleteBlocker`, `provider.SessionsStateUpdatedMsg`, `provider.RequestSessionsState`, `blockerform.Start`, `router.BlockerFormView`, `Session.OpenBlockers()`, `SessionBlocker.Label()`, `common.TimeAgo`.
- Produces: none consumed by later tasks.

- [ ] **Step 1: Write failing tests** — append to `model_test.go`:

```go
func makeBlockedSession() session.Session {
	s := makeActiveSession()
	s.Blockers = []session.SessionBlocker{
		{Model: gorm.Model{ID: 10}, Kind: session.BlockerKindReview, Note: "first"},
		{Model: gorm.Model{ID: 11}, Kind: session.BlockerKindSlack, Note: "second"},
	}
	return s
}

func runeKey(r string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}
}

func TestSessionDetail_RendersOpenBlockers(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	out := m.View()
	assert.Contains(t, out, "Blockers")
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
}

func TestSessionDetail_ResolveHighlightedBlocker(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("j"))
	_, cmd := m.Update(runeKey("u"))
	require.NotNil(t, cmd)
	msg := cmd()
	assert.Equal(t, "provider.resolveBlockerIntentMsg", reflect.TypeOf(msg).String())
	assert.Contains(t, fmt.Sprintf("%+v", msg), "blockerID:11")
}

func TestSessionDetail_DeleteBlockerNeedsTwoPresses(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, cmd := m.Update(runeKey("x"))
	assert.Nil(t, cmd)
	_, cmd = m.Update(runeKey("x"))
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.deleteBlockerIntentMsg", reflect.TypeOf(cmd()).String())
}

func TestSessionDetail_RefreshClampsCursorWhenLastBlockerResolved(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("j"))

	now := time.Now()
	refreshed := makeBlockedSession()
	refreshed.Blockers[0].ResolvedAt = &now
	refreshed.Blockers[1].ResolvedAt = &now
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{refreshed}})

	assert.NotContains(t, m.View(), "Blockers")
	_, cmd := m.Update(runeKey("u"))
	assert.Nil(t, cmd)
	_, cmd = m.Update(runeKey("x"))
	assert.Nil(t, cmd)
}

func TestSessionDetail_BlockKeyOpensForm(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	_, cmd := m.Update(runeKey("b"))
	require.NotNil(t, cmd)
}

func TestRenderPanel_ShowsBlockersWithoutCursor(t *testing.T) {
	s := makeBlockedSession()
	out := RenderPanel(&s, nil, 60, 30)
	assert.Contains(t, out, "first")
	assert.NotContains(t, out, "›")
}
```

Add imports `"fmt"`, `"reflect"`, `"time"`, `"github.com/eleonorayaya/utena/internal/tui/provider"`.

- [ ] **Step 2: Run `task test`** — expect failures.

- [ ] **Step 3: Keys** — add to `keyMap` in `keys.go`: `Up`, `Down`, `Block`, `Resolve`, `RemoveBlocker`; include `Block, Resolve, RemoveBlocker` in `ShortHelp` and all in `FullHelp`:

```go
	Up:            key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "prev blocker")),
	Down:          key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "next blocker")),
	Block:         key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "block")),
	Resolve:       key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "resolve blocker")),
	RemoveBlocker: key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete blocker")),
```

- [ ] **Step 4: Model** — add fields:

```go
type Model struct {
	sess                   *session.Session
	prs                    []git.PullRequest
	pendingDeleteID        uint
	interactive            bool
	blockerCursor          int
	pendingBlockerDeleteID uint
	width                  int
	height                 int
}
```

`Init` re-requests cached state so the view refreshes after returning from the blocker form:

```go
func (m Model) Init() (Model, tea.Cmd) {
	if m.sess == nil {
		return m, nil
	}
	return m, provider.RequestSessionsState()
}
```

In `Update`, in `SelectMsg` set `m.interactive = true`, `m.blockerCursor = 0`, `m.pendingBlockerDeleteID = 0`. Add a case:

```go
	case provider.SessionsStateUpdatedMsg:
		if m.sess == nil {
			return m, nil
		}
		for _, s := range msg.Sessions {
			if s.ID == m.sess.ID {
				fresh := s
				m.sess = &fresh
				break
			}
		}
		m.clampBlockerCursor()
		return m, nil
```

Add helpers:

```go
func (m *Model) clampBlockerCursor() {
	n := len(m.sess.OpenBlockers())
	if m.blockerCursor >= n {
		m.blockerCursor = n - 1
	}
	if m.blockerCursor < 0 {
		m.blockerCursor = 0
	}
}

func (m Model) selectedBlocker() *session.SessionBlocker {
	open := m.sess.OpenBlockers()
	if m.blockerCursor < 0 || m.blockerCursor >= len(open) {
		return nil
	}
	b := open[m.blockerCursor]
	return &b
}
```

In `onKeyMsg`, after the existing `pendingID := ...` lines add:

```go
	pendingBlockerID := m.pendingBlockerDeleteID
	m.pendingBlockerDeleteID = 0
```

and add cases:

```go
	case key.Matches(msg, keys.Up):
		if m.blockerCursor > 0 {
			m.blockerCursor--
		}
		return m, nil

	case key.Matches(msg, keys.Down):
		if m.blockerCursor < len(m.sess.OpenBlockers())-1 {
			m.blockerCursor++
		}
		return m, nil

	case key.Matches(msg, keys.Block):
		if m.sess.Status == session.StatusDeleted {
			return m, nil
		}
		return m, tea.Sequence(
			router.NavigateTo(router.BlockerFormView),
			blockerform.Start(m.sess.ID),
		)

	case key.Matches(msg, keys.Resolve):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		return m, provider.ResolveBlocker(m.sess.ID, b.ID)

	case key.Matches(msg, keys.RemoveBlocker):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		if pendingBlockerID == b.ID {
			return m, provider.DeleteBlocker(m.sess.ID, b.ID)
		}
		m.pendingBlockerDeleteID = b.ID
		return m, nil
```

- [ ] **Step 5: View** — insert after the Workspace/PR block (before the `$ Tmux` section):

```go
	if open := s.OpenBlockers(); len(open) > 0 {
		b.WriteString("\n" + sectionStyle().Render("⏸ Blockers") + "\n")
		for i, bl := range open {
			prefix := "  "
			if m.interactive && i == m.blockerCursor {
				prefix = lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("› ")
			}
			line := fmt.Sprintf("%s · %s · %s", bl.Kind, bl.Label(), common.TimeAgo(bl.CreatedAt))
			if m.pendingBlockerDeleteID == bl.ID {
				line += warningStyle().Render("  press x again to delete")
			}
			b.WriteString(prefix + valueStyle().Render(line) + "\n")
		}
	}
```

Import `common` and `blockerform`. `RenderPanel` builds `Model{...}` without `interactive`, so the panel shows no cursor.

- [ ] **Step 6: Run `task test`**, then `task fmt` and `task lint` — all clean.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/sessiondetail
git commit -m "feat(tui): show and resolve blockers in session detail"
```

---

### Task 7: Final verification

- [ ] **Step 1:** `task fmt && task lint && task test` — all clean.
- [ ] **Step 2:** Manual smoke (dev daemon): `task daemon:run` in one pane; in another run `utena sessions block <name> --kind review --url https://example.com`, confirm `utena sessions ls` shows `blocked`, `utena sessions blockers <name>` lists it, TUI shows the session under **Blocked**, `u` in detail resolves it and it returns to the main group.
- [ ] **Step 3:** Commit any fmt/lint fixes.
