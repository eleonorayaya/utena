# Plan: Workspace-Prefixed Session IDs with Display Name

## Context

Session IDs are globally unique and used directly as Zellij session names. Creating a "main" session in workspace "utena" prevents creating "main" in workspace "other". Solution: encode workspace name into `session.ID` (e.g., `utena-main`) so it remains globally unique, and add a `Name` field for the user-visible short name (e.g., `main`).

**Data flow change:**
- Current: user enters "main" → `ID = "main"` → Zellij session = "main"
- New: user enters "main" in workspace "utena" → `Name = "main"`, `ID = "utena-main"` → Zellij session = "utena-main"

No store, API route, or Zellij integration changes needed — `ID` remains the globally unique key everywhere.

## Changes

### 1. Session model — add Name field

**File:** `internal/session/session.go`

Add `Name string` field to `Session` struct. Add `BuildSessionID(workspaceName, name string) string` helper: sanitize workspace name (lowercase, spaces→hyphens), return `{sanitized}-{name}`.

### 2. SessionService — compute ID from workspace + name

**File:** `internal/session/sessionservice.go`

In `CreateSession`, after workspace lookup: if `session.Name` is set and workspace exists, compute `session.ID = BuildSessionID(ws.Name, session.Name)`. If no workspace, `session.ID = session.Name`.

### 3. TUI client — send Name, parse back ID

**File:** `internal/tui/client.go`

- `createSession` — send `name` field in request body (the short name). Parse `id` from response (the computed prefixed ID).
- `sessionCreatedMsg` — add `id` field (the full prefixed session ID).

### 4. TUI app — use response ID for activation/pipes

**File:** `internal/tui/app.go`

- `sessionCreatedMsg` handler — use `msg.id` for `activateSession()` call and store in pending
- `sessionActivatedMsg` handler — use the ID (prefixed) for pipe commands
- `pendingSession` — add `id` field to carry the server-assigned prefixed ID

### 5. TUI session list — display Name, operate on ID

**File:** `internal/tui/sessionlist.go`

- `sessionItem.Title()` — use `session.Name` if set, fall back to `session.ID`
- `FilterValue()` — use `session.Name` if set, fall back to `session.ID`
- All operational messages (activate, delete) — keep using `session.ID` (unchanged)
- Status messages to user — show `session.Name` instead of `session.ID`

### 6. TUI name input — emit Name, not ID

**File:** `internal/tui/nameinput.go`

`createSessionMsg` already sends `name` — no structural change needed. The name input captures the short name; the server computes the full ID.

### 7. Session types — add Name to request

**File:** `internal/session/types.go`

No changes needed — `CreateSessionRequest` embeds `*Session`, which now has `Name`. The `Name` field will be sent in the JSON body and bound automatically.

### 8. ZellijService — no changes

Zellij matching already uses `sess.ID`. Since `ID` is now the prefixed name, Zellij sessions naturally match. Sessions discovered from Zellij (not created by utena) get `ID` = raw Zellij name, `Name` = same.

### 9. Validation — validate Name, not ID

**File:** `internal/session/validation.go`

`ValidateSession` should validate `session.Name` (user input) rather than `session.ID` (computed). The ID is system-generated and may contain characters from workspace name. Add `ValidateSessionName` call on `session.Name` when it's set.

### 10. SessionStore migration

**File:** `internal/session/sessionstore.go`

In `OnAppStart`, for loaded sessions missing `Name`, set `Name = ID` (backward compat — old sessions where ID was the user-entered name).

### 11. Tests — minor updates

Set `Name` on test sessions. For sessions created through the service, verify `ID` is computed as `{workspace}-{name}`. Existing tests that add directly to store just need `Name` populated.

## File Summary

| File | Action |
|------|--------|
| `internal/session/session.go` | Add `Name` field, `BuildSessionID()` helper |
| `internal/session/sessionservice.go` | Compute `ID` from workspace + `Name` in `CreateSession` |
| `internal/session/sessionstore.go` | Migration: set `Name = ID` for old sessions |
| `internal/session/validation.go` | Validate `Name` instead of `ID` in `ValidateSession` |
| `internal/tui/client.go` | Send `name`, parse `id` from response |
| `internal/tui/app.go` | Use response ID for activation, add `id` to pending |
| `internal/tui/sessionlist.go` | Display `Name`, operate on `ID` |
| Test files | Set `Name` on test sessions, verify ID computation |

## Verification

1. `task daemon:build tui:build` — compilation
2. `task test` — all tests pass
3. Manual: create "main" in workspace A and workspace B — both succeed
4. Manual: Zellij sessions named `{workspace}-main`
5. Manual: TUI shows short name "main" with workspace in description
6. Manual: delete/switch operates on correct prefixed session
