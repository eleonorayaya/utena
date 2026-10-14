# Split `internal/tmux` into `internal/mux` + `internal/tuios`

## Goal

`internal/tmux` mixes two things: utena's own model of the terminal sessions it
owns (records, status, windows cache, attach hooks, activation long-poll) and
the tuios control-socket protocol. Split them so the multiplexer can be swapped
by writing one driver, and drop the stale "tmux" name everywhere, including
persisted and wire names.

This is a pure refactor. It must not change behavior. It is the base for
`utena top` (per-session resource usage), which lands in a follow-up PR.

## Packages

### `internal/mux` (multiplexer-agnostic, owns the domain)

- `Driver` interface, the only seam a multiplexer implements:

  ```go
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

- `Session` model (was `TmuxSession`), `SessionStatus` + constants, `Window`,
  `HookEvent`, errors (`ErrSessionNotFound`, `ErrSessionAlreadyExists`,
  `ErrNotAvailable`).
- `Store` (was `TmuxStore`), `Service` (was `TmuxService`), `Controller`,
  `Router`, `Module` with the same responsibilities and public methods as today.
  The service consumes `Driver` and `Event` instead of `tmuxRunner` and
  `tuiosEvent`. `handleTuiosEvent` becomes a switch over `EventType`.
- `FakeDriver` in `internal/mux/testing.go` (same place `MockRunner` lives
  today) replaces `MockRunner`. A separate `muxtest` package would create an
  import cycle with `mux`'s internal tests. Agent-meta helpers move to `tuios`
  tests, which use the existing fake tuios socket daemon.
- `Session` declares `TableName()` explicitly. Otherwise GORM would map it to
  `sessions` and collide with `session.Session`.

### `internal/tuios` (only knows the socket protocol)

- `Driver` implements `mux.Driver`. It contains the current `tuiosRunner`,
  socket path resolution, the JSON call framing and `tuiosError`.
- Event mapping: `Subscribe` follows the same tuios event list as today and
  translates it. `subscribed` and `gap` map to `EventResync`. `session-created`
  and `session-closed` map one to one. Window and workspace events map to
  `EventWindowsChanged`.
- Agent-pane tagging (`list-agents` / `set-agent-meta`) is tuios-only and moves
  into the driver. It runs on `agent-state` events and on every resync, as it
  does today, and is never surfaced to `mux`.
- The CLI helpers used directly by `cmd/tui` (`SwitchWorkspace`,
  `NewWorkspaceWindow`, `CloseWindow`) move here unchanged, along with
  `workspaces.go`. They are tuios-workspace concepts with no `mux` equivalent.

`internal/api/app.go` builds `mux.NewModule(tuios.NewDriver(), ...)`.
Neither `mux` nor anything else under `internal/` except `api` imports `tuios`.
`cmd/tui` imports `tuios` only for the workspace helpers.

## Renames on the wire and in storage

| What | Before | After |
|---|---|---|
| Table | `tmux_sessions` | `mux_sessions` |
| Column | `sessions.tmux_session_id` | `sessions.mux_session_id` |
| Go field | `Session.TmuxSessionID`, `Session.TmuxSession` | `MuxSessionID`, `MuxSession` |
| JSON | `tmux_session_id`, `tmux_session` | `mux_session_id`, `mux_session` |
| Routes | `/tmux/*` | `/mux/*` (hard cut, no alias) |
| Events | `tmux.session_created` and the others | `mux.session_created` and the others; Go constants `Mux*` |
| Helpers | `SanitizeTmuxName`, `GetByTmuxSessionID` | `SanitizeMuxName`, `GetByMuxSessionID` |

`cmd/tui/attach.go` and `internal/shellinit` switch to `/mux/*`. There is no
`/tmux` alias. After upgrading, restart any running `utena attach`, because an
old one long-polls `/tmux/activations/next` and will get 404s. Shells started
before the upgrade lose `UTENA_SESSION_ID` lookup until they are reopened.

The TUI reads `tmux_session` JSON (`statusview`, session list). Those readers
move to `mux_session` in the same change, so client and daemon must be upgraded
together. That is already how `task tui:install` and `task daemon:install`
are used.

## Migration

GORM `AutoMigrate` would create an empty `mux_sessions` table and a new
`mux_session_id` column, orphaning existing rows. The renames must run first.

- `db.DB` gains pre-migrations: `RegisterPreMigrations(fns ...func(gorm.Migrator) error)`,
  run inside `Migrate` before `AutoMigrate` (still under
  `PRAGMA foreign_keys=OFF`).
- Modules opt in through a new optional interface in `internal/common`,
  `PreMigrationProvider { PreMigrations() []func(gorm.Migrator) error }`,
  registered in `app.go` next to `ModelProvider`.
- Implementation note: the signature became `db.PreMigration func(tx *gorm.DB) error`.
  Pre-migrations need the transaction itself for data `UPDATE`s, and they reach
  schema operations through `tx.Migrator()`. All pre-migrations run in one
  transaction, with a `foreign_key_check` gate before commit, and
  `sqlite_sequence` is preserved across GORM's table rebuilds.
- `mux.Module`: if `tmux_sessions` exists and `mux_sessions` does not, rename
  the table. SQLite keeps index names across a table rename, so also drop
  `idx_tmux_sessions_name`, `idx_tmux_sessions_status` and
  `idx_tmux_sessions_deleted_at`. `AutoMigrate` recreates them under
  `idx_mux_sessions_*`.
- `session.Module`: if `sessions` has `tmux_session_id`, rename it to
  `mux_session_id` and drop the old unique index (`idx_sessions_tmux_session_id`).
  `AutoMigrate` then creates `idx_sessions_mux_session_id`. The belongs-to FK
  (`fk_sessions_tmux_session`) is dealt with explicitly: either rebuild
  `sessions` with the FK renamed, or confirm that GORM's sqlite migrator
  rebuilds it cleanly on its own, and pin that behavior in the migration test.
- Every step is idempotent, so a second start is a no-op.
- `Store.BackfillStatus` is a legacy `is_alive` fix-up. It keeps working
  against `mux_sessions` because the rename runs before it.

## Docs

Update `CLAUDE.md`, `README.md`, `docs/architecture.md`,
`docs/backend-patterns.md` and the `.claude/skills/go-patterns` examples to
describe `mux` and `tuios` and the `/mux` routes. Historical plans and specs
stay as written.

External dependency: the dotfiles tuios config (`after-attach` hook, popups,
dock cell) is outside this repo. Its only direct route dependency is the
`after-attach` hook in `programs/tuios/contents/config.toml`, which curls
`/tmux/hooks/client-session-changed`. It moves to `/mux/hooks/...` in a
companion dotfiles change, deployed together with the daemon upgrade.

## Testing

- Moved tests keep passing unchanged except for renames: service tests via
  `mux.FakeDriver`, store tests, and `tuiosclient_test` / `workspaces_test`
  under `tuios`.
- New `tuios` test: event translation (each raw tuios event maps to the
  expected `mux.Event`, and `agent-state` triggers tagging but emits nothing).
- New migration test: seed an in-memory DB by `AutoMigrate`-ing copies of the
  old struct definitions, so it gets GORM's real index and FK names. Insert
  rows, then run the module migrations. Assert all of the following:
  - Rows are intact under the new names.
  - `PRAGMA index_list` and `PRAGMA foreign_key_list` show no `tmux` names.
  - There is exactly one FK on `sessions.mux_session_id`, and it references
    `mux_sessions`.
  - A second run is a no-op.
- Router test: `/mux/...` serves the hook, env and activation endpoints, and
  `/tmux/...` returns 404.
- `task test` and `task lint` pass.
- Smoke test that never touches the live DB or the real tuios:
  1. Back up `utena.db`.
  2. Run the new daemon against a second copy, with `UTENA_DATA_DIR` set to the
     copy's directory, a spare `UTENA_PORT`, and `TUIOS_SOCKET` pointing at a
     bogus path.
  3. Diff `.schema` and per-table row counts before and after.
  4. Only then run `task daemon:install`.

## Out of scope

`utena top`, `Driver.Panes()` and resource sampling (next PR). No behavior
changes to session lifecycle, events or the TUI.
