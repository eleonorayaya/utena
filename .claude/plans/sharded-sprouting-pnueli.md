# Plan: SQLite + GORM Persistence Layer

## Context

All backend state is currently stored in in-memory maps with JSON file persistence. Each store loads a JSON file on startup, holds data in a `map[string]*Model` with `sync.RWMutex`, and writes back to JSON on every mutation. This is fragile (no transactions, no query capability, no concurrent access safety beyond mutex). Migrating to SQLite via GORM gives us proper relational persistence with minimal operational overhead.

## Design Decisions

- **String primary keys** (not `gorm.Model`): All current IDs are caller-generated strings. Each model declares `ID string` with `gorm:"primaryKey"`. GORM auto-fills `CreatedAt`/`UpdatedAt` on `time.Time` fields without needing `gorm.Model`.
- **Resources as JSON column**: Session's `Resources` struct is always read/written as a unit, never queried independently. Use `gorm:"serializer:json"` tag.
- **WorkspaceName via JOIN**: Instead of resolving workspace names in the service layer with N+1 queries, add a `Workspace` association field to Session and Todo models. Use GORM's `Joins("Workspace")` to LEFT JOIN and populate `Workspace.Name` in a single query. The `WorkspaceName` JSON field is populated from the joined Workspace in an `AfterFind` hook or at the store level after query. Remove `resolveWorkspaceName()` from services.
- **Workspace config.json stays as file**: The `config.json` with `workspace_roots` and `workspaces` arrays is configuration, not domain data. Only `Workspace` records move to SQLite.
- **DB interface for testability**: Define a `Database` interface in `internal/db/` that exposes the GORM operations stores need. Stores depend on this interface, not the concrete `*DB` type. Tests can use in-memory SQLite via the same interface, or provide a mock if needed.
- **CGO SQLite driver**: Use `gorm.io/driver/sqlite` (wraps `github.com/mattn/go-sqlite3`) for production performance. Requires `CGO_ENABLED=1`.
- **TmuxSessionName uniqueness**: Use a nullable-friendly unique index. Sessions with empty `TmuxSessionName` will store empty string (SQLite allows this with careful handling, but current code always sets it).

## Steps

### Step 1: Add dependencies

```
go get gorm.io/gorm gorm.io/driver/sqlite
```

### Step 2: Create `internal/db/` package

**New file: `internal/db/db.go`**

Define a `Database` interface that abstracts the GORM operations stores need. Stores depend on this interface; the concrete `DB` struct implements it.

```go
type Database interface {
    Create(value any) *gorm.DB
    Save(value any) *gorm.DB
    Delete(value any, conds ...any) *gorm.DB
    First(dest any, conds ...any) *gorm.DB
    Find(dest any, conds ...any) *gorm.DB
    Where(query any, args ...any) *gorm.DB
    Model(value any) *gorm.DB
    Order(value any) *gorm.DB
    Migrate(models ...any) error
    Close() error
}

type DB struct {
    *gorm.DB
}

func Open(dbPath string) (*DB, error)       // opens file at dbPath, WAL mode, MaxOpenConns(1), busy_timeout=5000
func OpenInMemory() (*DB, error)             // file::memory:?cache=shared for tests
func (db *DB) Migrate(models ...any) error   // calls AutoMigrate
func (db *DB) Close() error                  // closes underlying sql.DB
```

The `Database` interface returns `*gorm.DB` from query-building methods (Where, Order, Model) to allow chaining. This is intentional — the interface abstracts the database lifecycle (Open/Close/Migrate) and entry points, while GORM's fluent API handles query composition.

Open configuration:
- `gorm.Config{SkipDefaultTransaction: true}` for performance
- After open: `PRAGMA journal_mode=WAL`, `PRAGMA foreign_keys=ON`, `PRAGMA busy_timeout=5000`
- `sqlDB.SetMaxOpenConns(1)` (SQLite is single-writer)

### Step 3: Add GORM tags and associations to model structs

**`internal/workspace/workspace.go`** - add `gorm:"primaryKey"`, `gorm:"uniqueIndex"` on Path

**`internal/session/session.go`**:
- Add `gorm:"primaryKey"` on ID, `gorm:"uniqueIndex"` on TmuxSessionName, `gorm:"index"` on WorkspaceID
- Add `gorm:"serializer:json"` on Resources
- Add `Workspace *Workspace` association field with `gorm:"foreignKey:WorkspaceID"` for JOIN loading
- Change `WorkspaceName` to `gorm:"-"` (populated from `Workspace.Name` after query)

**`internal/todo/todo.go`**:
- Add `gorm:"primaryKey"` on ID, `gorm:"index"` on WorkspaceID
- Add `Workspace *Workspace` association field with `gorm:"foreignKey:WorkspaceID"` for JOIN loading
- Change `WorkspaceName` to `gorm:"-"` (populated from `Workspace.Name` after query)

**`internal/claude/claudesession.go`** - add `gorm:"primaryKey"`, `gorm:"index"` on SessionID

### Step 4: Rewrite `WorkspaceStore`

**File: `internal/workspace/workspacestore.go`**

Replace `map + mutex + JSON` internals with `db.Database`. Keep `loadConfig()`/`saveConfig()`/`discoverWorkspaces()`/`isGitRepository()`/`expandHome()`/`generateID()` (these use `afero.Fs` for config.json and filesystem access).

Constructor: `NewWorkspaceStore(database db.Database, fs afero.Fs, configDir string)`

Method mapping:
| Current | GORM |
|---------|------|
| `GetByID(id)` | `db.First(&ws, "id = ?", id)` |
| `GetByPath(path)` | `db.First(&ws, "path = ?", path)` |
| `List()` | `db.Find(&workspaces)` + sort in Go (preserve current sort: last_used first, then alpha) |
| `Add(ws)` | `db.Create(ws)` |
| `Update(ws)` | `db.Save(ws)` |
| `AddWorkspace(path)` | create + `db.Create` + update config.json |
| `AddWorkspaceRoot(path)` | scan dir + `db.Create` each + update config.json |
| `OnAppStart` | `discoverWorkspaces()` → upsert each into DB |
| `OnAppEnd` | no-op |

Remove: `sync.RWMutex`, `map[string]*Workspace`, `load()`, `save()`, `workspacesPath()`

### Step 5: Rewrite `SessionStore`

**File: `internal/session/sessionstore.go`**

Constructor: `NewSessionStore(database db.Database)`

Method mapping:
| Current | GORM |
|---------|------|
| `GetByID(id)` | `db.Joins("Workspace").First(&session, "sessions.id = ?", id)` → map `gorm.ErrRecordNotFound` to `ErrSessionNotFound`. Populate `WorkspaceName` from joined `Workspace.Name`. |
| `GetByTmuxName(name)` | `db.Joins("Workspace").First(&session, "tmux_session_name = ?", name)` |
| `List()` | `db.Joins("Workspace").Order("sessions.last_used_at DESC").Find(&sessions)` → populate `WorkspaceName` from joined workspace |
| `ListByWorkspace(wsID)` | `db.Joins("Workspace").Where("sessions.workspace_id = ?", wsID).Order("sessions.last_used_at DESC").Find(&sessions)` |
| `Add(session)` | `db.Create(session)` (omit Workspace association with `db.Omit("Workspace")`) |
| `Update(session)` | `db.Omit("Workspace").Save(session)` with exists check |
| `Delete(id)` | `db.Delete(&Session{}, "id = ?", id)` with exists check |
| `OnAppStart` | no-op (migration handled by db.Migrate) |
| `OnAppEnd` | no-op |

After each read query, populate `WorkspaceName` from the joined `Workspace` field (helper method on the store).

Remove: `sync.RWMutex`, `map[string]*Session`, `afero.Fs`, `configDir`, `save()`, `sessionsPath()`, deep-copy logic, `legacySession` struct

### Step 6: Rewrite `TodoStore`

**File: `internal/todo/todostore.go`**

Constructor: `NewTodoStore(database db.Database)`

Method mapping:
| Current | GORM |
|---------|------|
| `GetByID(id)` | `db.Joins("Workspace").First(&todo, "todos.id = ?", id)` → map to `ErrTodoNotFound`. Populate `WorkspaceName` from joined workspace. |
| `List()` | `db.Joins("Workspace").Order("todos.created_at DESC").Find(&todos)` → populate `WorkspaceName` |
| `ListByWorkspaceID(wsID)` | `db.Joins("Workspace").Where("todos.workspace_id = ?", wsID).Order(...).Find(&todos)` |
| `Add(t)` | `db.Omit("Workspace").Create(t)` |
| `Delete(id)` | `db.Delete(&Todo{}, "id = ?", id)` with exists check |
| `OnAppStart/OnAppEnd` | no-op |

After each read query, populate `WorkspaceName` from the joined `Workspace` field.

Remove: `sync.RWMutex`, `map`, `afero`, `configDir`, `save()`, `todosPath()`

### Step 7: Rewrite `ClaudeStore`

**File: `internal/claude/claudestore.go`**

Constructor: `NewClaudeStore(database db.Database)`

Method mapping:
| Current | GORM |
|---------|------|
| `GetByID(id)` | `db.First(&cs, "id = ?", id)` → map to `ErrClaudeSessionNotFound` |
| `List()` | `db.Order("last_updated_at DESC").Find(&sessions)` |
| `ListBySessionID(sid)` | `db.Where(...).Order(...).Find(&sessions)` |
| `Upsert(session)` | `db.Save(session)` (GORM Save does upsert on PK) |
| `UpdateStatusBySessionID(sid, from, to)` | `db.Model(&ClaudeSession{}).Where("session_id = ? AND status = ?", sid, from).Updates(map[string]any{...})` |
| `Delete(id)` | `db.Delete(&ClaudeSession{}, "id = ?", id)` with exists check |
| `OnAppStart/OnAppEnd` | no-op |

Remove: `sync.RWMutex`, `map`, `afero`, `configDir`, `save()`, `sessionsPath()`

### Step 8: Update module constructors and `app.go`

**`internal/api/app.go`**:
- Add `db db.Database` field to `App`
- `NewApp`: create `db.Open(filepath.Join(cfg.ConfigDir, "utena.db"))`, call `db.Migrate(...)` with all 4 model types
- Pass `database` to module constructors instead of `fs`+`configDir` (workspace still gets `fs`+`configDir` for config.json)
- `OnEnd`: call `db.Close()`

**Module constructor changes**:
- `workspace.NewWorkspaceModule(database, fs, configDir)` - store gets db + keeps fs/configDir
- `session.NewSessionModule(tmuxManager, workspaceModule, bus, database, branchPrefix)` - drops `fs`, `configDir`
- `todo.NewTodoModule(workspaceModule, database)` - drops `fs`, `configDir`
- `claude.NewClaudeModule(bus, database)` - drops `fs`, `configDir`
- `NewTestApp`: uses `db.OpenInMemory()`, still uses `afero.NewMemMapFs()` for workspace config

### Step 9: Cleanup

- Remove `afero` imports from session, todo, claude packages
- Remove `workspaces.json` persistence code from workspace (only config.json remains)
- Delete the `legacySession` struct and migration code from sessionstore
- Remove `resolveWorkspaceName()` from `SessionService` and `TodoService` — workspace name is now populated by the store via JOINs
- Remove `workspaceService` dependency from `SessionService` and `TodoService` where it was only used for name resolution (check if it's still needed for other operations)

## Files to Modify

| File | Change |
|------|--------|
| `go.mod` | Add gorm.io/gorm, gorm.io/driver/sqlite |
| `internal/db/db.go` | **NEW** - Database interface, DB struct, Open, OpenInMemory, Migrate, Close |
| `internal/workspace/workspace.go` | Add GORM tags |
| `internal/session/session.go` | Add GORM tags + `Workspace` association field |
| `internal/todo/todo.go` | Add GORM tags + `Workspace` association field |
| `internal/claude/claudesession.go` | Add GORM tags |
| `internal/workspace/workspacestore.go` | Rewrite to use `db.Database` |
| `internal/session/sessionstore.go` | Rewrite to use `db.Database` with Joins("Workspace") |
| `internal/session/sessionservice.go` | Remove `resolveWorkspaceName()` |
| `internal/todo/todostore.go` | Rewrite to use `db.Database` with Joins("Workspace") |
| `internal/todo/todoservice.go` | Remove `resolveWorkspaceName()` |
| `internal/claude/claudestore.go` | Rewrite to use `db.Database` |
| `internal/workspace/workspacemodule.go` | Update constructor signature |
| `internal/session/sessionmodule.go` | Update constructor signature |
| `internal/todo/todomodule.go` | Update constructor signature |
| `internal/claude/claudemodule.go` | Update constructor signature |
| `internal/api/app.go` | Add DB creation, update wiring, close on shutdown |

## Verification

1. `task fmt` - code compiles and formats
2. `task daemon:run` - daemon starts, creates `utena.db` in config dir
3. Manual API test: `curl localhost:3333/workspaces` returns discovered workspaces
4. Manual API test: create a session via POST, verify it persists across daemon restart
5. `task test` - existing tests pass with in-memory SQLite
