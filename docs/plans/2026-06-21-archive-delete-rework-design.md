# Archive / Delete UX Rework — Design

Date: 2026-06-21

## Problem

Today the session list's primary destructive action (`d`) hard-deletes any
session, soft-deleting the row (`StatusDeleted`) while deliberately leaving the
worktree directory on disk as a durable artifact. Archive exists but is a
secondary, lesser-used path. We want archiving to be the primary action and
deletion to be a deliberate, fully-cleaning second step that only applies to
already-archived sessions.

## Goals

1. The primary session-list action (`d`) **archives** a live session.
2. **Delete is only allowed on already-archived sessions** (plus a force escape
   hatch for stuck `creating` sessions).
3. **Delete cleans up every associated resource gracefully** — a failure on one
   resource logs a warning and continues with the rest.
4. **Archive** kills the tmux session but leaves all other resources alive
   (already true today).
5. **Archived sessions are filtered from the list by default**, with a toggle
   key to reveal them.

## Decisions (from product owner)

- **Archive funnel accepts broken too.** `active`, `inactive`, `completed`, and
  `broken` sessions can be archived. `creating`/`pending` keep their existing
  direct escape hatches (force-delete / dismiss) — they have no real resources.
- **Contextual single key.** `d` archives a live session; on an
  already-archived session (visible via the toggle) `d` deletes it. A separate
  key toggles archived visibility.
- **Delete removes worktree directories.** Explicit delete is the one path that
  fully tears down on-disk worktrees. This is **irreversible** — uncommitted
  work in a worktree is lost. Branch deletion stays governed by the existing
  `delete_branch` query flag. (This intentionally diverges, for the explicit
  user-delete path only, from commit `ed4cc31`, which made dirs durable across
  *reconcile / branch-change*; that path is unchanged.)

## Behavior by status (the `d` key)

| Status                                   | `d` action            |
|------------------------------------------|-----------------------|
| active / inactive / completed / broken   | Archive (confirm)     |
| archived                                 | Delete (confirm)      |
| creating                                 | Force-delete (confirm)|
| pending                                  | Dismiss               |
| attached (any)                           | Blocked               |

## Service layer

### `ArchiveSession`
Add `StatusBroken` to the set of archivable statuses. Otherwise unchanged: kill
tmux, null `TmuxSessionID`, set `StatusArchived`, clear `IsAttached`.

### `DeleteSession(ctx, id, deleteBranch, force)`
- Reject if attached (`ErrSessionAttached`).
- Gate: allow only when `status == StatusArchived` **or** `force` (the escape
  hatch the TUI uses for `creating`). New error `ErrSessionNotArchived`
  ("only archived sessions can be deleted") otherwise.
- Graceful cleanup (`cleanupSessionResources`), each step logs+continues:
  1. Kill tmux session if `TmuxSessionID != nil`.
  2. For each worktree: `git worktree remove --force` the dir, then
     `CleanupBranch` (honours `deleteBranch`).
  3. `os.RemoveAll(SessionRoot)` — guarded so it only fires for paths under
     `sessionsRoot`.
- Hard-delete the session row (`SessionStore.HardDelete`, `Unscoped`). FK
  `ON DELETE CASCADE` physically removes join rows, claude sessions, actions,
  and setup steps.
- Then delete the now-unreferenced worktree records
  (`GitService.DeleteWorktree`) — must come **after** the cascade so the
  `RESTRICT` FK on `session_worktrees.worktree_id` is satisfied.

### New store methods
- `SessionStore.HardDelete(id uint) error` — `Unscoped().Delete`.
- `GitService.DeleteWorktree(id uint) error` — wraps `worktreeStore.Delete`
  (already a hard delete).

## TUI (`internal/tui/sessionlist`)

- `rebuildFiltered`: hide `StatusArchived` unless `showArchived`. (Still always
  hide `StatusDeleted` for safety, though hard-delete makes it rare.)
- New `showArchived` field + toggle key (`a`, "toggle archived"), mirroring the
  existing `showBroken` / `.` pattern.
- `Close` (`d`) handler becomes contextual per the table above, with distinct
  confirm prompts:
  - live → "press d again to archive <name>"
  - archived → "press d again to delete <name> (removes worktrees)"
  - creating → existing force-delete prompt
  - pending → dismiss immediately
- Provider already exposes `ArchiveSession`; a new `DismissSession` command is
  wired to the existing `PUT /sessions/{id}/dismiss` route (needed because the
  new delete gate would otherwise reject `pending` sessions via `d`, a
  regression). The contextual handler dispatches archive / delete / force-delete
  / dismiss by status.

### Session detail view (`internal/tui/sessiondetail`)
The detail view keeps its separate `a` (archive) / `d` (delete) keys, brought in
line with the new rule: archive now also accepts `broken`; delete is a no-op
unless the session is `archived` (or `creating`, via the force hatch).

## Testing

- Service: broken is archivable; delete rejects non-archived without force;
  delete of an archived session removes worktree dir + branch and hard-deletes
  the row (GetByID → not found); cleanup continues when tmux kill fails; force
  still deletes a creating session.
- Rewrite `TestSessionService_DeleteSession_PreservesWorktreeDirOnDisk` (now the
  opposite contract) and `TestSessionService_DeleteSession` (row gone, not
  `StatusDeleted`).
- TUI: `d` on a live session emits archive; `d` on an archived session emits
  delete; archived hidden by default and revealed by the toggle.

## Out of scope

- Reconcile / branch-change worktree durability (unchanged).
- `pending` dismiss and `creating` force semantics (only re-routed through the
  contextual key, logic unchanged).
