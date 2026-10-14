# mux / tuios Split Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `internal/tmux` with a multiplexer-agnostic `internal/mux`
(domain, store, service, routes, `Driver` interface) and an `internal/tuios`
driver. Rename every persisted and wire name from tmux to mux, with an
in-place DB migration and no behavior change.

**Spec:** `docs/superpowers/specs/2026-10-13-mux-tuios-split-design.md`. Read it
first.

**Tech stack:** Go, chi, GORM + `gorm.io/driver/sqlite` v1.6, testify.

**Rules (from `CLAUDE.md`):**
- Never run `go` directly. Use `task test`, `task lint` and `task fmt` from the
  repo root.
- No code comments. Delete the existing doc comments on `RegisterPending` and
  `SpawnForRecord` only if you rewrite those functions; otherwise move them
  verbatim.
- Every task ends green (`task test` passes) and with one commit. Commit
  messages end with
  `Co-Authored-By: Claude Opus 5.5 (1M context) <noreply@anthropic.com>`.

**Critical gotchas:**
1. GORM derives table names from struct names. `mux.Session` would map to
   `sessions` and collide with `session.Session`. Give it
   `TableName() string` from the very first task.
2. Until Task 5, the DB schema must not change at all. In Tasks 1–4,
   `mux.Session.TableName()` returns `"tmux_sessions"` and
   `session.Session` keeps its `TmuxSessionID` / `TmuxSession` fields.
3. Index and FK names are derived from table and field names. Task 5 deals
   with all of them.

---

### Task 1: Move `internal/tmux` → `internal/mux` (rename only)

Pure rename: the same code under new package and identifier names, with the
tuios client still inside. The schema and the wire stay unchanged.

**Files:**
- `git mv internal/tmux internal/mux`. Rename the files:
  `tmuxcontroller.go→controller.go`, `tmuxmodule.go→module.go`,
  `tmuxrouter.go→router.go`, `tmuxservice.go→service.go`,
  `tmuxsession.go→session.go`, `tmuxstore.go→store.go`, plus the matching
  `_test.go` files. Leave `tuiosclient*.go`, `workspaces*.go`, `testing.go`,
  `types.go` and `backfill_test.go` as they are.
- Modify every importer: `internal/api/app.go`, `internal/api/server_test.go`,
  `internal/session/{session.go,sessionmodule.go,sessionservice.go}` and its
  tests (`sessionservice_test.go`, `sessionservice_addworkspace_test.go`,
  `sessionservice_pr_test.go`, `sessionstore_test.go`,
  `sessionrouter_test.go`), `internal/common/tracked/tracked_test.go`,
  `internal/tui/statusview/{sessiontab.go,statusview.go}`,
  `cmd/tui/{main.go,workspace.go}`.

**Step 1: Apply this identifier map inside the package and at every call
site.** Import alias `utmux` becomes `mux`.

| Old | New |
|---|---|
| `package tmux` | `package mux` |
| `TmuxSession` | `Session` |
| `TmuxSessionStatus` | `SessionStatus` |
| `TmuxStatusPending/Active/Inactive` | `StatusPending/Active/Inactive` |
| `ErrTmuxSessionNotFound` | `ErrSessionNotFound` |
| `ErrTmuxSessionAlreadyExists` | `ErrSessionAlreadyExists` |
| `ErrTmuxNotAvailable` | `ErrNotAvailable` |
| `TmuxStore`, `NewTmuxStore` | `Store`, `NewStore` |
| `TmuxService`, `NewTmuxService` | `Service`, `NewService` |
| `TmuxController`, `NewTmuxController` | `Controller`, `NewController` |
| `TmuxRouter`, `NewTmuxRouter` | `Router`, `NewRouter` |
| `TmuxModule`, `NewTmuxModule`, `NewTmuxModuleWithRunner` | `Module`, `NewModule`, `NewModuleWithRunner` |
| `App.Tmux` field (`internal/api/app.go`) | `App.Mux` |

Error strings: `"tmux session not found"` → `"mux session not found"`,
`"tmux session already exists"` → `"mux session already exists"`. Update the
`Store.Add` conflict message the same way. Grep the tests for asserted strings
before changing any of them.

**Step 2: Pin the table name.** Add this to `internal/mux/session.go`:

```go
func (Session) TableName() string { return "tmux_sessions" }
```

**Step 3: Leave these unchanged for now:** route mount path `/tmux` (rename the
module entry only to `{"mux", "/tmux", a.Mux}`), eventbus constants, and
`session.Session.TmuxSessionID` / `TmuxSession` fields. The field type becomes
`*mux.Session`, but keep `gorm:"foreignKey:TmuxSessionID"` exactly as it is.

**Step 4:** `task fmt && task test && task lint`. Expect PASS with the same
test count as before.

**Step 5:** Run `git grep -n 'internal/tmux'`. Expect hits only in historical
docs (`docs/plans`, `docs/superpowers/plans`, `.claude/plans`,
`docs/retrospectives`). Leave `CLAUDE.md` and `docs/` for Task 6.

**Step 6: Commit** `refactor: rename internal/tmux package to internal/mux`.

---

### Task 2: Introduce `mux.Driver` and move the tuios client to `internal/tuios`

**Files:**
- Create: `internal/mux/driver.go`
- Modify: `internal/mux/service.go`, `internal/mux/module.go`,
  `internal/mux/testing.go`, `internal/mux/service_test.go`
- Move: `internal/mux/tuiosclient.go` → `internal/tuios/driver.go`,
  `internal/mux/tuiosclient_test.go` → `internal/tuios/driver_test.go`
- Modify: `internal/api/app.go`, `internal/api/server_test.go`, session tests
  using `NewMockRunner` / `MockRunner`, and
  `internal/common/tracked/tracked_test.go`

**Step 1: Create `internal/mux/driver.go`.**

```go
package mux

import "context"

type Driver interface {
	NewSession(name, startDir string, env map[string]string) error
	NewWindow(session, startDir, command string, env map[string]string) error
	KillSession(name string) error
	HasSession(name string) bool
	ListSessions() ([]string, error)
	ListWindows(session string) ([]Window, error)
	Subscribe(ctx context.Context, onEvent func(Event)) error
}

type EventType string

const (
	EventResync         EventType = "resync"
	EventSessionCreated EventType = "session_created"
	EventSessionClosed  EventType = "session_closed"
	EventWindowsChanged EventType = "windows_changed"
)

type Event struct {
	Type    EventType
	Session string
}
```

**Step 2: Write failing tuios tests** in `internal/tuios/driver_test.go`. Reuse
the existing `startFakeTuios` helper, and extend it if it can't stream events.
- `TestDriver_SubscribeTranslatesEvents`: the fake daemon acks, then streams
  `session-created`, `session-closed`, `window-created`,
  `workspace-switched` and `gap`. Assert that the collected `[]mux.Event` is
  `[{resync ""}, {session_created s}, {session_closed s}, {windows_changed s}, {windows_changed s}, {resync ""}]`.
  The first `resync` comes from the subscribe ack.
- `TestDriver_AgentStateTagsPaneAndEmitsNothing`: port
  `TestTmuxService_AgentStateTagsPaneWithSessionName`. An `agent-state`
  event with state `working` produces one `set-agent-meta` request
  (`session: work, window: w1, tokens: {session: work}`). State `none`
  produces no request, and no `mux.Event` is emitted.
- `TestDriver_ResyncTagsUntaggedAgentPanes`: port
  `TestTmuxService_TagAgentPanesSkipsAlreadyTagged`. On subscribe, the driver
  calls `list-agents` and tags only the pane whose meta lacks the session.

Run `task test`. Expect FAIL to compile, because `tuios.Driver` doesn't exist
yet.

**Step 3: Implement `internal/tuios/driver.go`** from the old `tuiosclient.go`.
- `package tuios`. Rename `tuiosRunner` → `Driver`, `newTuiosRunner()` →
  `NewDriver() *Driver` and `tuiosSocketPath` → `socketPath`. Keep
  `call`, `dial`, `tuiosError`, `isTuiosCode`, `envArgv` and `loginShell`
  unexported.
- Export the methods so they satisfy `mux.Driver`: `NewSession`, `NewWindow`,
  `KillSession`, `HasSession`, `ListSessions` (was `listSessionNames`),
  `ListWindows` (returns `[]mux.Window`).
- Move `followedTuiosEvents`, `agentSessionMetaKey`, `tagAgentPanes` and
  `tagAgentPane` here from `service.go`. They become `Driver` methods that use
  `d.call("list-agents"...)` / `d.call("set-agent-meta"...)`.
- Add `var _ mux.Driver = (*Driver)(nil)`.
- `Subscribe(ctx, onEvent func(mux.Event))` keeps the old socket loop and
  dispatches each raw event through `d.translate(ev, onEvent)`:

```go
func (d *Driver) translate(ev tuiosEvent, onEvent func(mux.Event)) {
	switch ev.Type {
	case tuiosEventSubscribed, "gap":
		d.tagAgentPanes()
		onEvent(mux.Event{Type: mux.EventResync})
	case "session-created":
		onEvent(mux.Event{Type: mux.EventSessionCreated, Session: ev.Session})
	case "session-closed":
		onEvent(mux.Event{Type: mux.EventSessionClosed, Session: ev.Session})
	case "agent-state":
		d.tagAgentPane(ev.Session, ev.Window, ev.State)
	default:
		onEvent(mux.Event{Type: mux.EventWindowsChanged, Session: ev.Session})
	}
}
```

  Note the ordering. The old service called `resync()` and then
  `tagAgentPanes()`. Tagging first and then emitting resync is equivalent,
  because tagging touches no utena state.
- Move `CloseWindow` and all of `workspaces.go` / `workspaces_test.go` into
  `internal/tuios` unchanged except for the package name. They construct
  `&Driver{socketPath: socketPath()}`.

**Step 4: Rewrite `internal/mux/service.go` against `Driver`.**
- Field `runner tmuxRunner` → `driver Driver`. `NewService(driver Driver, store *Store, bus eventbus.EventBus)`.
- `followEvents` calls `t.driver.Subscribe(ctx, func(ev Event) {...})`. Change
  the log text from "tuios event stream unavailable" to
  "mux event stream unavailable".
- Rename `handleTuiosEvent(ctx, ev tuiosEvent)` → `handleEvent(ctx, ev Event)`:

```go
switch ev.Type {
case EventResync:
	t.resync()
case EventSessionCreated:
	err = t.HandleSessionCreated(ctx, ev.Session)
	t.refreshWindows(ev.Session)
case EventSessionClosed:
	err = t.HandleSessionClosed(ctx, ev.Session)
case EventWindowsChanged:
	t.refreshWindows(ev.Session)
}
```

- `resync()` loses its trailing `t.tagAgentPanes()` call.
- Replace every `t.runner.x(...)` with the exported driver method, and every
  `t.runner == nil` check with `t.driver == nil`.
- `module.go`: `NewModule(driver Driver, bus, database)` and
  `NewModuleWithDriver(driver Driver, store *Store, bus)`, replacing
  `NewModuleWithRunner`.

**Step 5: Rewrite `internal/mux/testing.go`** as `FakeDriver` /
`NewFakeDriver()`, implementing `Driver` with exported methods. Keep the
`Sessions`, `SpawnedWindows`, `CreateErr`, `KillErr`, `OnNewSession`,
`OnKillSession`, `SetCreateErr`, `SetKillErr`, `HasSessionByName` and
`RemoveSession` members. Drop `Agents`, `AgentMetaWrites`, `AgentMetaWrite`
and `MetaWrites`. `Subscribe` blocks until `ctx.Done()`. Delete the two
agent-tagging tests from `service_test.go`; they now live in tuios. Add
`var _ Driver = (*FakeDriver)(nil)`.

**Step 6: Update call sites.**
- `internal/api/app.go`: `mux.NewModule(tuios.NewDriver(), bus, database)`.
- Tests: `mux.NewMockRunner()` → `mux.NewFakeDriver()`, `*mux.MockRunner`
  → `*mux.FakeDriver`, `NewModuleWithRunner` → `NewModuleWithDriver`.
- `cmd/tui/main.go` and `cmd/tui/workspace.go`: `tmux.CloseWindow` /
  `SwitchWorkspace` / `NewWorkspaceWindow` → `tuios.X`.

**Step 7: Check the dependency boundary.**
`git grep -n 'internal/tuios"' -- internal cmd` must list only
`internal/api/app.go`, `cmd/tui/*.go` and `internal/tuios` itself.
`git grep -n 'tuios' internal/mux` must return only log/error wording you have
consciously kept. Change `ErrNotAvailable`'s message to
`"terminal multiplexer is not available"`.

**Step 8:** `task fmt && task test && task lint`. Expect PASS.

**Step 9: Commit** `refactor(mux): extract Driver interface; move tuios client to internal/tuios`.

---

### Task 3: Rename routes and eventbus names to mux

**Files:** `internal/eventbus/events.go`, `internal/session/sessionservice.go`,
`internal/mux/service.go`, `internal/api/app.go`, `cmd/tui/attach.go`,
`internal/shellinit/shellinit.go`, tests that reference these.

**Step 1: Write a failing router test** in
`internal/api/server_test.go`, using the existing test-app helper:
- `PUT /mux/hooks/client-attached` with `{"session_name":"x"}` returns 200.
- `GET /mux/sessions/nope/env/K` returns 404, matching current behavior for an
  unknown session.
- `PUT /tmux/hooks/client-attached` returns 404.

Run `task test`. Expect FAIL.

**Step 2:**
- `app.go` module entry: `{"mux", "/mux", a.Mux}`.
- `eventbus/events.go`: `TmuxSessionCreated` → `MuxSessionCreated`
  (`"mux.session_created"`), and likewise for `SessionClosed`,
  `ClientSessionChanged`, `ClientAttached` and `ClientDetached`.
  `TmuxHookEvent{TmuxSessionName}` → `MuxHookEvent{SessionName}`.
- `sessionservice.go`: update the subscriptions and type assertions. Rename
  the handlers `handleTmuxSessionCreated` → `handleMuxSessionCreated` and so
  on, and update their tests.
- `cmd/tui/attach.go`: `/tmux/activations/next` → `/mux/activations/next`,
  and `/tmux/hooks/%s` → `/mux/hooks/%s`.
- `shellinit.go`: `/tmux/sessions/` → `/mux/sessions/`. Update any shellinit
  test.

**Step 3:** `task fmt && task test && task lint`. Expect PASS.

**Step 4: Commit** `refactor: serve multiplexer API at /mux and rename tmux.* events`.

---

### Task 4: Pre-migration hook in `internal/db`

**Files:** `internal/db/dbservice.go`, `internal/common/lifecycle.go`,
`internal/api/app.go`. Test: `internal/db/dbservice_test.go` (create).

**Step 1: Write a failing test.** `TestDB_PreMigrationsRunBeforeAutoMigrate`
opens `db.OpenInMemory()` and creates table `legacy_widgets(id integer primary key, name text)` with one row via `Exec`. It registers a
pre-migration that calls `m.RenameTable("legacy_widgets", "widgets")` and
registers model `type widget struct{ ID uint; Name string }`. It then calls
`OnAppStart` and asserts that `SELECT count(*) FROM widgets` returns 1.

**Step 2: Implement.**

```go
type PreMigration func(m gorm.Migrator) error

func (d *DB) RegisterPreMigrations(fns ...PreMigration) {
	d.preMigrations = append(d.preMigrations, fns...)
}
```

`OnAppStart` runs `Migrate(d.models...)` whenever there are pre-migrations or
models. `Migrate` runs every pre-migration (with `d.Migrator()`) after
`PRAGMA foreign_keys=OFF` and before `AutoMigrate`, and returns the first
error wrapped as `fmt.Errorf("pre-migration %d: %w", i, err)`.

In `common/lifecycle.go`:

```go
type PreMigrationProvider interface {
	PreMigrations() []db.PreMigration
}
```

If that creates an import cycle (`db` importing `common`), declare the
interface in `internal/api/app.go` instead, unexported. In `buildApp`, after
`RegisterModels`, collect `PreMigrations()` from modules in `modules()`
order and register them.

**Step 3:** `task test`. Expect PASS.

**Step 4: Commit** `feat(db): support pre-migrations that run before AutoMigrate`.

---

### Task 5: Rename persisted names with migration

**Files:** `internal/mux/session.go`, `internal/mux/module.go`,
`internal/mux/store.go` (`BackfillStatus` SQL),
`internal/session/{session.go,sessionstore.go,sessionmodule.go,sessionservice.go}`
plus every test touching `TmuxSessionID` / `TmuxSession` / `SanitizeTmuxName` /
`GetByTmuxSessionID`, the TUI readers of `Session.TmuxSession`
(`git grep -n 'TmuxSession' internal/tui cmd`). Test:
`internal/api/migration_test.go` (create).

**Step 1: Write the failing migration test** in
`internal/api/migration_test.go`. In the test file, define copies of the
**old** structs exactly as they are today, so GORM generates the real legacy
names:

```go
type legacyTmuxSession struct {
	gorm.Model
	Name     string            `gorm:"uniqueIndex"`
	StartDir string
	Env      map[string]string `gorm:"serializer:json"`
	Status   string            `gorm:"index"`
}

func (legacyTmuxSession) TableName() string { return "tmux_sessions" }

type legacySession struct {
	gorm.Model
	Name          string
	TmuxSessionID *uint              `gorm:"uniqueIndex"`
	TmuxSession   *legacyTmuxSession `gorm:"foreignKey:TmuxSessionID"`
}

func (legacySession) TableName() string { return "sessions" }
```

Test body:
1. `gormDB := db.OpenInMemorySQLite()`, then
   `gormDB.AutoMigrate(&legacyTmuxSession{}, &legacySession{})`. Insert a
   tmux row `work` (active) and a session row `s1` linked to it.
2. `app := newApp(gormDB, afero.NewMemMapFs(), testCfg)`. Use the same cfg
   helper as `server_test.go`. Then call `app.DB.OnAppStart(ctx)`.
3. Assert:
   - `app.Session.Service` can load `s1`, its `MuxSession.Name == "work"`, and
     its status is preserved.
   - The raw query `SELECT name FROM sqlite_master WHERE sql LIKE '%tmux%'`
     returns no rows. That covers tables, indexes and FK constraint text.
   - `PRAGMA foreign_key_list(sessions)` has exactly one row with
     `from = mux_session_id` and `table = mux_sessions`.
   - Calling `app.DB.OnAppStart(ctx)` a second time succeeds and changes
     nothing (row counts are the same).
4. Add a second test, `TestMigration_FreshDB`: `newApp` on an empty DB, then
   `OnAppStart` succeeds and produces `mux_sessions`.

Run `task test`. Expect FAIL.

**Step 2: Rename the code.**
- `mux.Session.TableName()` → `"mux_sessions"`. In `store.go`, change
  `BackfillStatus` SQL from `tmux_sessions` to `mux_sessions`.
- `session.Session`: `TmuxSessionID *uint json:"tmux_session_id,omitempty" gorm:"uniqueIndex"` →
  `MuxSessionID *uint json:"mux_session_id,omitempty" gorm:"uniqueIndex"`, and
  `TmuxSession` → `MuxSession *mux.Session json:"mux_session,omitempty" gorm:"foreignKey:MuxSessionID"`.
- `sessionstore.go`: `Joins("TmuxSession")` → `Joins("MuxSession")`,
  `Omit(..., "TmuxSession")` → `"MuxSession"`, `GetByTmuxSessionID` →
  `GetByMuxSessionID` with `"sessions.mux_session_id = ?"`, and update the
  conflict message wording.
- `SanitizeTmuxName` → `SanitizeMuxName`. Rename local variables named
  `tmuxName`, `tmuxRecord` and `tmuxAlive` to `muxName`, `muxRecord` and
  `muxAlive` where you touch them (optional but preferred). Rename the
  `tracedOp` labels `"tmux new-session"` / `"tmux spawn-for-record"` to
  `"mux ..."`.

**Step 3: Add the pre-migrations.**

`mux.Module`:

```go
func (m *Module) PreMigrations() []db.PreMigration {
	return []db.PreMigration{func(mg gorm.Migrator) error {
		if !mg.HasTable("tmux_sessions") || mg.HasTable("mux_sessions") {
			return nil
		}
		if err := mg.RenameTable("tmux_sessions", "mux_sessions"); err != nil {
			return err
		}
		for _, idx := range []string{"idx_tmux_sessions_name", "idx_tmux_sessions_status", "idx_tmux_sessions_deleted_at"} {
			if mg.HasIndex("mux_sessions", idx) {
				if err := mg.DropIndex("mux_sessions", idx); err != nil {
					return err
				}
			}
		}
		return nil
	}}
}
```

`session.Module` runs after `mux` (it comes later in `modules()`; verify the
order and move it if needed). Its pre-migration does the following:
1. Return early if `!mg.HasColumn(&legacy, "tmux_session_id")`. Use a small
   unexported `legacySessionsTable` struct with
   `TableName() "sessions"` and a `TmuxSessionID *uint` field, so the
   migrator has a model to resolve against.
2. If `mg.HasConstraint("sessions", "fk_sessions_tmux_session")`, call
   `mg.DropConstraint("sessions", "fk_sessions_tmux_session")`. The sqlite
   migrator does this by recreating the table.
3. If `mg.HasIndex("sessions", "idx_sessions_tmux_session_id")`, drop it.
4. `mg.RenameColumn(&legacySessionsTable{}, "tmux_session_id", "mux_session_id")`.

`AutoMigrate` then adds `idx_sessions_mux_session_id` and
`fk_sessions_mux_session`. If the test shows the sqlite migrator can't take a
table name for `HasConstraint` / `DropConstraint` / `HasIndex`, pass the
legacy struct instead. Let the test decide, and don't guess.

**Step 4:** `task test`. Iterate until the migration test passes with no `tmux`
left in `sqlite_master`.

**Step 5:** `task fmt && task lint`, then
`git grep -n -i tmux -- internal cmd`. Expect hits only in the migration code,
its test and `BackfillStatus`'s legacy `is_alive` path. Anything else is a
missed rename.

**Step 6: Commit** `refactor: rename tmux persisted names to mux with in-place migration`.

---

### Task 6: Docs

**Files:** `CLAUDE.md`, `README.md`, `docs/architecture.md`,
`docs/backend-patterns.md`, `docs/adding-features.md` (check it),
`.claude/skills/go-patterns/{store-patterns.md,testing.md}`.

**Step 1:** Replace descriptions of `internal/tmux`, `TmuxService`,
`tmuxRunner`/`MockRunner` and `/tmux` routes with `internal/mux`
(`Service`, `Driver`, `FakeDriver`, `/mux`) and `internal/tuios` (the
`Driver` implementation and workspace helpers). In `CLAUDE.md`, replace the
"Tmux Service" key-pattern paragraph with a "Multiplexer" paragraph that
explains the `Driver` seam. Leave historical `docs/plans`,
`docs/superpowers/{plans,specs}` (other than this one), `.claude/plans` and
`docs/retrospectives` untouched.

**Step 2: Commit** `docs: describe mux/tuios split`.

---

### Task 7: Verification

**Step 1:** `task test` and `task lint`. Expect PASS. Paste the summary.

**Step 2: Smoke-test the migration on a copy of the real DB.** Never touch the
live file. Find the live data dir (`UTENA_DATA_DIR`, otherwise the default in
`internal/api/config.go`).

```bash
SMOKE=$(mktemp -d)
cp "<live-data-dir>/utena.db" "$SMOKE/utena.db"
cp "$SMOKE/utena.db" "$SMOKE/before.db"
```

Run the new daemon (`task daemon:run` or the built binary) with
`UTENA_DATA_DIR=$SMOKE UTENA_PORT=3399 TUIOS_SOCKET=$SMOKE/none.sock`. Wait for
"Initialized module", then stop it. Then:

```bash
sqlite3 "$SMOKE/before.db" "select count(*) from tmux_sessions; select count(*) from sessions where tmux_session_id is not null;"
sqlite3 "$SMOKE/utena.db"  "select count(*) from mux_sessions;  select count(*) from sessions where mux_session_id is not null;"
sqlite3 "$SMOKE/utena.db"  "select name from sqlite_master where sql like '%tmux%';"
```

Expected: the counts match and the last query is empty.

Also diff the before and after copies on all of these:
- `pragma_table_info` for every table
- row counts for every table
- every index in `sqlite_master`, especially any index on `sessions` that the
  current model doesn't declare
- `PRAGMA foreign_key_check`
- `sqlite_sequence`, where each `seq` must be at least its old value

Expected: the only differences are the intended renames and drops.

**Step 3:** Report results. Do **not** run `task daemon:install` yet. The user
deploys the daemon, TUI and the dotfiles `after-attach` change
(`dotfiles/programs/tuios/contents/config.toml`, already edited and
uncommitted) together, then restarts `utena attach`.
