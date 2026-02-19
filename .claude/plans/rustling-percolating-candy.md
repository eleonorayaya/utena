# Session Picker: Fix Activation & Error Feedback

## Context

Two bugs in the session picker:

1. **Sessions don't activate in Zellij when selected** — `ActivateSession` and `CreateSessionAndNotify` in `sessionservice.go` ignore the error returned by `eventBus.Publish()`. The event bus is synchronous — it runs the pipe command to Zellij inline — so if the pipe fails, the error is silently swallowed and the HTTP handler returns 200. The TUI quits thinking success. Additionally, `CreateSessionAndNotify` hardcodes an empty `WorkspacePath` (line 81), so new sessions never get their CWD set in Zellij.

2. **No error feedback when session creation 400s** — `createSession()` in `client.go` doesn't parse the server's error body (just formats the status code). `App.Update()` silently discards all `errMsg` (`_ = msg.err` at line 81). The error never reaches `NameInputModel` which already has red error display capability.

---

## Step 1: Add `ErrSessionNotFound` sentinel

**File**: `internal/session/session.go`

Add alongside existing `ErrSessionAlreadyExists` (line 8):
```go
var ErrSessionNotFound = errors.New("session not found")
```

**File**: `internal/session/sessionstore.go`

Update `GetByID` (line 36) and `Update` (line 127) to use:
```go
return nil, ErrSessionNotFound
```

---

## Step 2: Propagate event bus errors in SessionService

**File**: `internal/session/sessionservice.go`

### ActivateSession (line 118)
Check the `Publish` return value:
```go
if err := s.eventBus.Publish(ctx, eventbus.Event{...}); err != nil {
    return nil, fmt.Errorf("failed to notify plugin: %w", err)
}
```

### CreateSessionAndNotify (lines 77-84)
1. Resolve `WorkspaceID` → filesystem path via `workspaceStore.GetByID()`:
```go
var workspacePath string
if session.WorkspaceID != "" {
    if ws, err := s.workspaceStore.GetByID(session.WorkspaceID); err == nil {
        workspacePath = ws.Path
    }
}
```
2. Use the resolved path in the event instead of `""`
3. Check the `Publish` return value (same pattern as ActivateSession)

---

## Step 3: Differentiate error types in ActivateSession controller

**File**: `internal/session/sessioncontroller.go` (lines 126-140)

Currently returns 404 for all errors. Use the sentinel to differentiate:
```go
if errors.Is(err, ErrSessionNotFound) {
    render.Render(w, r, common.ErrNotFound())
} else {
    render.Render(w, r, common.ErrUnknown(err))
}
```

---

## Step 4: Parse error response body in TUI client

**File**: `internal/tui/client.go`

Add a helper to extract server error messages:
```go
func parseAPIError(res *http.Response, fallback string) errMsg {
    var errResp struct {
        Error string `json:"error"`
    }
    if json.NewDecoder(res.Body).Decode(&errResp) == nil && errResp.Error != "" {
        return errMsg{errors.New(errResp.Error)}
    }
    return errMsg{fmt.Errorf("%s: unexpected status %d", fallback, res.StatusCode)}
}
```

Use in `createSession()` (line 113) and `activateSession()` (line 88) instead of the generic format string.

---

## Step 5: Route errors to NameInputModel in App

**File**: `internal/tui/app.go` (lines 80-82)

Replace the silent discard with routing to the active view:
```go
case errMsg:
    if a.activeView == nameInputView {
        a.nameInput.err = msg.err.Error()
        return a, nil
    }
    return a, nil
```

`NameInputModel` already renders `err` in red via `errStyle` (nameinput.go:81-83). No changes needed there.

---

## Files to modify

| File | Change |
|------|--------|
| `internal/session/session.go` | Add `ErrSessionNotFound` sentinel |
| `internal/session/sessionstore.go` | Use `ErrSessionNotFound` in GetByID, Update |
| `internal/session/sessionservice.go` | Check Publish errors; resolve workspace path |
| `internal/session/sessioncontroller.go` | Differentiate 404 vs 500 in ActivateSession |
| `internal/tui/client.go` | Parse error response body from server |
| `internal/tui/app.go` | Route errMsg to active view |

---

## Verification

1. `task daemon:build && task tui:build` — both compile
2. Run existing tests: `go test ./internal/session/... ./internal/zellij/... ./internal/workspace/...`
3. Manual test in Zellij dev environment (`task dev`):
   - Select existing session → should switch (or show meaningful error if pipe fails)
   - Create session with duplicate name → should show server error in red text
   - Create session with valid name → should create and switch to it with correct CWD
