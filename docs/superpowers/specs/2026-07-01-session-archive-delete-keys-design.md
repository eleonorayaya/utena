# Separate archive / delete keys in the session list

## Goal

Give the session list two distinct destructive shortcuts instead of today's
single contextual `d`:

- **`a`** — archive the selected session
- **`d`** — hard-delete the selected session (removes worktrees), works on any
  session in one step

## Background

The backend, controller, and TUI provider already support both operations:

- `SessionService.DeleteSession(ctx, id, deleteBranch, force)` — `force=true`
  bypasses the archive-first guard and hard-deletes an active/creating session
  (`internal/session/sessionservice.go:1132`).
- `DeleteSession` controller honors `?force=true` (`sessioncontroller.go:134`).
- `provider.DeleteSession(id, force)` and `provider.ArchiveSession(id)` exist
  (`internal/tui/provider/sessionsprovider.go:58`).

So this is a **TUI-only** change in `internal/tui/sessionlist/`. No backend,
controller, or provider changes.

## Final keymap (`internal/tui/sessionlist/keys.go`)

| Key | Action | Change |
|-----|--------|--------|
| `a` | Archive selected session | was: toggle archived view |
| `d` | Delete selected session (force) | was: contextual archive/delete |
| `.` | Toggle hidden (broken **and** archived) | was: toggle broken only |

- Remove the `ToggleArchived` binding entirely.
- `Close` binding renamed to `Delete` (`d`, help text `"delete"`); add a new
  `Archive` binding (`a`, help text `"archive"`).
- `ToggleBroken` renamed to `ToggleHidden` (`.`, help text `"toggle hidden"`).
- Update `ShortHelp`/`FullHelp` lists accordingly.

## Behavior (`internal/tui/sessionlist/sessionlist.go`)

### State

- Collapse `showBroken` + `showArchived` into a single `showHidden bool`.
- Add `pendingArchiveID uint` alongside the existing `pendingDeleteID uint`.

### `rebuildFiltered`

Skip `StatusDeleted` always; skip `StatusBroken` and `StatusArchived` unless
`showHidden` is set.

### Confirm-reset logic (top of `OnKeyMsg`)

Generalize the existing reset: on any key that is not `Archive` and not nav,
clear `pendingArchiveID`; on any key that is not `Delete` and not nav, clear
`pendingDeleteID`. Cursor movement clears both (unchanged).

### `a` (Archive)

- `sel == nil` → not handled.
- attached → status `"cannot archive attached session"`.
- already `StatusArchived` → status `"already archived"`.
- first press → set `pendingArchiveID`, status `"press a again to archive <name>"`.
- second press (same id) → clear pending, `provider.ArchiveSession(id)`.

### `d` (Delete)

- `sel == nil` → not handled.
- attached → status `"cannot delete attached session"`.
- `StatusPending` → `provider.DismissSession(id)` (holds no resources).
- first press → set `pendingDeleteID`, status
  `"press d again to delete <name> (removes worktrees)"`.
- second press (same id) → clear pending, `provider.DeleteSession(id, true)`.

`force=true` makes the delete path uniform, so the `CanDelete()` / `IsCreating()`
branching and the `closeConfirmMessage` / `forceDeleteMessage` helpers are
deleted.

## Tests (`internal/tui/sessionlist/sessionlist_test.go`)

Rework the existing delete tests to the new semantics:

- `a` first press on active → `pendingArchiveID` set, `"archive <name>"` message.
- `a` second press → `archiveSessionIntentMsg`.
- `a` on already-archived → `"already archived"`, no command.
- `d` first press on active → `pendingDeleteID` set, `"delete <name>"` message.
- `d` second press on active → `deleteSessionIntentMsg` (force delete works on a
  live session — the core new capability).
- `d` on pending → `dismissSessionIntentMsg`.
- attached session → `a` and `d` both blocked with status message, no command.
- `.` reveals both broken and archived; default filters both out.

Replace `TestSessionList_ToggleArchived_RevealsArchived` (asserts `showArchived`)
with a `showHidden` equivalent driven by `.`.

## Out of scope

- The full-screen **session detail** view (`sessiondetail/model.go`, opened with
  `i`) keeps its own contextual `d` flow. Mirror later if desired.
- No changes to `workspacelist` / `todolist` delete flows.
