# Plan: Task 1.7 — TmuxSession model + store

## Files to create

### 1. `internal/common/signals.go` (if it doesn't exist)

Create the Signal type and severity constants needed by TmuxSession.Signals():

```go
package common

type SignalSeverity string

const (
    SeverityInfo    SignalSeverity = "info"
    SeverityWarning SignalSeverity = "warning"
    SeverityUrgent  SignalSeverity = "urgent"
)

type Signal struct {
    Source   string         `json:"source"`
    Key      string         `json:"key"`
    Severity SignalSeverity `json:"severity"`
    Label    string         `json:"label"`
}
```

### 2. `internal/tmux/tmuxsession.go`

TmuxSession GORM model with `Signals()` method.

```go
package tmux

import (
    "fmt"

    "github.com/eleonorayaya/utena/internal/common"
    "gorm.io/gorm"
)

type TmuxSession struct {
    gorm.Model
    Name     string            `json:"name" gorm:"uniqueIndex"`
    StartDir string            `json:"start_dir"`
    Env      map[string]string `json:"env" gorm:"serializer:json"`
    IsAlive  bool              `json:"is_alive"`
}

func (ts *TmuxSession) Signals() []common.Signal {
    if !ts.IsAlive {
        return []common.Signal{
            {
                Source:   "tmux",
                Key:      fmt.Sprintf("tmux:%d", ts.ID),
                Severity: common.SeverityInfo,
                Label:    "tmux stopped",
            },
        }
    }
    return nil
}
```

### 3. `internal/tmux/tmuxstore.go`

CRUD store following the same pattern as `internal/session/sessionstore.go`:
- `NewTmuxStore(database db.Database) *TmuxStore`
- `Add(session *TmuxSession) error` — creates record, handles unique constraint errors
- `GetByID(id uint) (*TmuxSession, error)` — returns `ErrRecordNotFound`-wrapped error
- `GetByName(name string) (*TmuxSession, error)` — lookup by unique name
- `List() []TmuxSession` — returns all records
- `Update(session *TmuxSession) error` — saves changes
- `Delete(id uint) error` — soft-deletes by ID

Uses `gorm.ErrRecordNotFound` checks, same `isUniqueConstraintError` helper pattern. The store is simpler than SessionStore since TmuxSession has no associations to join/preload.

### 4. `internal/tmux/tmuxstore_test.go`

Tests using `setupTestDB` helper with `db.OpenInMemory()` and `database.Migrate(&TmuxSession{})`.

Test cases:
1. **Add and GetByID** — add session, retrieve by ID, verify fields
2. **GetByName** — add session, retrieve by name, verify correct session
3. **Name uniqueness** — add two sessions with same name, second returns error
4. **Update IsAlive** — add with IsAlive=false, update to true, verify
5. **Env JSON round-trip** — add session with env map `{"FOO":"bar","BAZ":"qux"}`, retrieve, verify map matches
6. **Delete** — add, delete, GetByID returns error
7. **List** — add multiple sessions, List returns all
8. **Signals alive** — session with IsAlive=true returns empty/nil slice
9. **Signals dead** — session with IsAlive=false returns one signal with Source="tmux", Severity=SeverityInfo, Label="tmux stopped"

## Verification

Run `task test -- ./internal/tmux/...` to confirm all tests pass.

## Notes

- The `db.Database` type is used as returned by `db.OpenInMemory()` (returns `*db.DB`). Looking at the session store pattern, `Database` appears to be a type that wraps `*gorm.DB` with methods like `First`, `Create`, `Save`, `Delete`, `Joins`, `Find`, `Migrate`, `Close`.
- No comments in code per project style guidelines.
- The store pattern closely mirrors `sessionstore.go` but is simpler (no joins/preloads needed).
