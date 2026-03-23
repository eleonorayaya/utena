# Plan: GORM Has-Many from Session to ClaudeSession

## Context

Currently `ClaudeSession.SessionID` is a string matching `Session.TmuxSessionName`. There's no GORM foreign key — the TUI correlates them client-side by fetching both datasets separately (`GET /sessions` + `GET /claude/sessions`) and building a `map[string][]claude.ClaudeSession`.

Additionally, the claude service subscribes to eventbus events to mark claude sessions as completed when the user switches tmux sessions. This couples claude session lifecycle to utena session navigation.

**Goals**:
1. Create a proper GORM has-many: `Session` has many `ClaudeSession`s via uint FK
2. `ClaudeSession` stays in the `claude` package — `session` imports `claude`, not the reverse
3. Claude sessions manage their own status purely via hooks — remove eventbus-driven status changes
4. Hook events send the utena session DB ID directly (not tmux name) — claude has zero knowledge of sessions
5. Eliminate separate TUI fetch for claude sessions (preload via has-many)

## Dependency direction

```
session  -->  claude      (for ClaudeSession type in has-many field)
session  -->  workspace   (existing)
claude   -/-> session     (claude has no concept of utena sessions — just stores a uint FK)
tui      -->  session     (gets claude sessions via preloaded Session.ClaudeSessions)
tui      -/-> claude      (no longer needed)
```

## Steps

### 1. Set `UTENA_SESSION_ID` to DB ID on tmux session creation

**`internal/session/sessionservice.go`** — `setupTmux()`:
After creating the tmux session, set the env var via tmux:
```go
func (s *SessionService) setupTmux(ctx context.Context, sess *Session) error {
    startDir := s.resolveStartDir(ctx, sess)
    if err := s.tmuxService.CreateSession(sess.TmuxSessionName, startDir); err != nil {
        return fmt.Errorf("failed to create tmux session: %v", err)
    }
    s.tmuxService.SetSessionEnv(sess.TmuxSessionName, "UTENA_SESSION_ID", fmt.Sprintf("%d", sess.ID))
    return nil
}
```

Also update `ActivateSession()` (line 430) which revives dead tmux sessions — set the env var there too.

**`internal/tmux/tmuxservice.go`** — add `SetSessionEnv` method:
```go
func (s *TmuxService) SetSessionEnv(sessionName, key, value string) {
    s.client.RunCommand("set-environment", "-t", sessionName, key, value)
}
```

### 2. Update `shellinit` to read from tmux environment

**`internal/shellinit/shellinit.go`**:
```go
func Script() string {
    return `if [ -n "$TMUX" ]; then
  _utena_val=$(tmux show-environment UTENA_SESSION_ID 2>/dev/null)
  if [ -n "$_utena_val" ] && [ "${_utena_val#-}" = "$_utena_val" ]; then
    export "$_utena_val"
  fi
  unset _utena_val
fi
`
}
```

(`tmux show-environment` returns `KEY=value` or `-KEY` if unset — the check ensures we only export valid assignments)

### 3. Update hook script to send uint session ID

**`plugins/utena-claude/hooks/utena-claude-hook.sh`**:
No structural change needed — it already sends `$UTENA_SESSION_ID` as `session_id`. The value is now a uint string instead of a tmux name.

### 4. Update `ClaudeSession` model (`internal/claude/claudesession.go`)

Change `SessionID` to uint. Add `StatusIdle` and `StatusDone` statuses. Remove `StatusCompleted`:

```go
const (
    StatusIdle           ClaudeSessionStatus = "idle"
    StatusWorking        ClaudeSessionStatus = "working"
    StatusNeedsAttention ClaudeSessionStatus = "needs_attention"
    StatusReadyForReview ClaudeSessionStatus = "ready_for_review"
    StatusDone           ClaudeSessionStatus = "done"
)

type ClaudeSession struct {
    gorm.Model
    ClaudeSessionID string              `json:"claude_session_id" gorm:"uniqueIndex"`
    SessionID       uint                `json:"session_id" gorm:"index"`
    Status          ClaudeSessionStatus `json:"status"`
    CWD             string              `json:"cwd,omitempty"`
}
```

### 5. Update `HookEventRequest` (`internal/claude/types.go`)

Change `SessionID` from string to uint:
```go
type HookEventRequest struct {
    Event            string `json:"event"`
    ClaudeSessionID  string `json:"claude_session_id"`
    SessionID        uint   `json:"session_id,string"`
    CWD              string `json:"cwd,omitempty"`
    NotificationType string `json:"notification_type,omitempty"`
}
```

Note: `json:"session_id,string"` handles JSON string→uint parsing since the hook script sends it as a string in JSON.

### 6. Add has-many field to `Session` (`internal/session/session.go`)

Import `claude` package, add field:
```go
ClaudeSessions []claude.ClaudeSession `json:"claude_sessions,omitempty" gorm:"foreignKey:SessionID"`
```

### 7. Update `SessionStore` (`internal/session/sessionstore.go`)

- Add `.Preload("ClaudeSessions")` to `List()`, `ListByWorkspace()`, `GetByID()`, `GetByTmuxName()`:
  ```go
  s.db.Joins("Workspace").Preload("ClaudeSessions").Order("sessions.last_used_at DESC").Find(&sessions)
  ```
- Add `Omit("ClaudeSessions")` alongside existing `Omit("Workspace")` in `Add()` and `Update()`

### 8. Restructure `ClaudeService` (`internal/claude/claudeservice.go`)

Remove eventbus dependency entirely. Simplify to just hook handling:

```go
type ClaudeService struct {
    store *ClaudeStore
}

func NewClaudeService(store *ClaudeStore) *ClaudeService {
    return &ClaudeService{store: store}
}

func (s *ClaudeService) HandleHookEvent(ctx context.Context, req *HookEventRequest) error {
    switch req.Event {
    case "SessionStart":
        return s.store.Create(&ClaudeSession{
            ClaudeSessionID: req.ClaudeSessionID,
            SessionID:       req.SessionID,
            Status:          StatusIdle,
            CWD:             req.CWD,
        })

    case "UserPromptSubmit", "PreToolUse":
        return s.store.UpdateStatus(req.ClaudeSessionID, StatusWorking)

    case "Stop", "TaskCompleted":
        return s.store.UpdateStatus(req.ClaudeSessionID, StatusReadyForReview)

    case "Notification":
        if req.NotificationType == "permission_prompt" {
            return s.store.UpdateStatus(req.ClaudeSessionID, StatusNeedsAttention)
        }
        return nil

    case "SessionEnd":
        return s.store.UpdateStatus(req.ClaudeSessionID, StatusDone)

    default:
        slog.Warn("unknown hook event", "event", req.Event)
        return nil
    }
}

func (s *ClaudeService) ListAll(ctx context.Context) ([]ClaudeSession, error) {
    return s.store.List(), nil
}

func (s *ClaudeService) ListBySessionID(ctx context.Context, sessionID uint) ([]ClaudeSession, error) {
    return s.store.ListBySessionID(sessionID), nil
}
```

### 9. Simplify `ClaudeStore` (`internal/claude/claudestore.go`)

Replace current methods with cleaner API:

- `Create(cs *ClaudeSession) error` — insert new record
- `UpdateStatus(claudeSessionID string, status ClaudeSessionStatus) error` — update by external ID
- `List() []ClaudeSession` — list all (keep as-is)
- `ListBySessionID(sessionID uint) []ClaudeSession` — change param from string to uint
- Remove `Upsert()` — creation and updates are separate paths
- Remove `UpdateStatusBySessionID()` — eventbus handlers removed
- Remove `DeleteByClaudeSessionID()` — SessionEnd now sets status to done instead of deleting

### 10. Update `ClaudeController` (`internal/claude/claudecontroller.go`)

- `ListClaudeSessionsBySession`: change route param `{sessionId}` to be a uint (parsed from URL). Call `service.ListBySessionID(ctx, uint)`.

### 11. Update `ClaudeModule` (`internal/claude/claudemodule.go`)

```go
func NewClaudeModule(database db.Database) *ClaudeModule {
    store := NewClaudeStore(database)
    service := NewClaudeService(store)
    ...
}
```

Remove `eventbus.EventBus` parameter. `OnAppStart`/`OnAppEnd` become no-ops (or delegate to store only). `Models()` stays as `[]any{&ClaudeSession{}}`.

### 12. Update `App` wiring (`internal/api/app.go`)

```go
Claude: claude.NewClaudeModule(database),
```

Remove `bus` from the call. The `session -> claude` import already exists implicitly via the Session struct.

### 13. Simplify TUI — use `session.ClaudeSessions` directly

Claude sessions are now nested on each session via the has-many preload. No separate map or fetch needed.

**`internal/tui/provider/client.go`**:
- Remove `fetchClaudeSessions()` method
- Remove `claude` import

**`internal/tui/provider/sessionsprovider.go`**:
- Remove `claudeSessionsLoadedMsg` type and its handler
- Remove `claudeSessions map[string][]claude.ClaudeSession` field from `sessionsProvider`
- Remove `ClaudeSessions` field from `SessionsStateUpdatedMsg` — sessions already carry their claude sessions
- In `fetchSessionsIntentMsg` handler: just `p.client.fetchSessions()` (drop `tea.Batch`)
- `sessionsLoadedMsg` handler simplifies to just store sessions and emit state

**`internal/tui/sessionlist/sessionlist.go`**:
- Remove `claudeSessions` map field from Model
- In `rebuildItems()`: use `s.ClaudeSessions` directly instead of looking up from a separate map
- In `SessionsStateUpdatedMsg` handler: just store sessions, no separate claude sessions to track

**`internal/tui/sessionlist/sessionitem.go`**:
- Change `claudeStatus` field from `string` to `claude.ClaudeSessionStatus`
- Replace `aggregateClaudeStatus()` (returns string) with a call to the shared `aggregateStatus()` pattern that returns `claude.ClaudeSessionStatus` (matching `statusview/model.go`'s existing approach)
- Move display formatting (e.g., `"[needs attention]"`) into `Title()` by switching on the enum
- Replace `StatusCompleted` → `StatusDone`, add `StatusIdle` handling

**`internal/tui/statusview/model.go`**:
- Remove `claudeSessions` map field
- Use `session.ClaudeSessions` directly when rendering session rows
- Replace `StatusCompleted` → `StatusDone` references
- Add handling for `StatusIdle`

### 14. SQLite migration (`internal/claude/claudestore.go`)

Drop the table on startup (data is transient — tracks active Claude Code instances):

```go
func (s *ClaudeStore) OnAppStart(ctx context.Context) error {
    s.db.Exec("DROP TABLE IF EXISTS claude_sessions")
    return nil
}
```

GORM's AutoMigrate will recreate with correct schema.

### 15. Update tests

**`internal/claude/claudestore_test.go`**:
- `setupTestDB`: also migrate a minimal Session model (for FK target)
- Create session records as FK targets
- Use uint session IDs
- Update to new store API (`Create`/`UpdateStatus`/`Delete`)
- Remove `UpdateStatusBySessionID` tests

**`internal/claude/claudeservice_test.go`**:
- Same DB setup
- Remove eventbus setup and all eventbus-related tests
- Test create-then-update-by-ID flow
- `SessionStart` creates record, subsequent events update by `ClaudeSessionID`

## Files changed

| File | Change |
|------|--------|
| `internal/session/sessionservice.go` | Set `UTENA_SESSION_ID` env var after tmux session creation |
| `internal/tmux/tmuxservice.go` | Add `SetSessionEnv()` method |
| `internal/shellinit/shellinit.go` | Read from tmux environment instead of deriving from session name |
| `internal/claude/claudesession.go` | `SessionID` string → uint |
| `internal/claude/types.go` | `SessionID` string → uint with `json:",string"` |
| `internal/session/session.go` | Add `ClaudeSessions` has-many field, import `claude` |
| `internal/session/sessionstore.go` | Preload on reads, Omit on writes |
| `internal/claude/claudestore.go` | New API (`Create`/`UpdateStatus`/`Delete`), uint SessionID, drop table migration |
| `internal/claude/claudeservice.go` | Remove eventbus, simplify to pure hook handler |
| `internal/claude/claudemodule.go` | Drop eventbus param |
| `internal/claude/claudecontroller.go` | Parse uint session ID from route param |
| `internal/api/app.go` | Remove bus from claude module |
| `internal/tui/provider/client.go` | Remove `fetchClaudeSessions` |
| `internal/tui/provider/sessionsprovider.go` | Remove claude sessions map/msg/fetch, sessions carry their own |
| `internal/tui/sessionlist/sessionlist.go` | Remove claude sessions map, use `s.ClaudeSessions` directly |
| `internal/tui/sessionlist/sessionitem.go` | `StatusCompleted` → `StatusDone`, add `StatusIdle` handling |
| `internal/tui/statusview/model.go` | Remove claude sessions map, use nested data, update status refs |
| `internal/claude/claudestore_test.go` | New store API, uint IDs |
| `internal/claude/claudeservice_test.go` | Remove eventbus tests, test hook flow |

## Verification

1. `task test` — all tests pass
2. `task fmt` — no formatting issues
3. `task daemon:run` + `task tui:run` — TUI shows session list with claude status indicators
4. Start a Claude Code session in a managed tmux session → verify `UTENA_SESSION_ID` is the DB uint ID (check with `echo $UTENA_SESSION_ID`)
5. Verify hook events update claude session status in the TUI
