# Session Blockers

## Goal

Track external things a session is waiting on (a code review, a reply to a Slack message, anything else) and separate blocked sessions in the UI. Blockers are added and resolved manually from the TUI and CLI; nothing is automated.

## Decisions

- A session may have zero or more blockers.
- "Blocked" is derived, not a `SessionStatus`: `Session.IsBlocked()` is true when any blocker is unresolved. `Status` and the session lifecycle (activate, archive, reconcile, repair, PR monitoring, merge → completed) are untouched.
- Resolving a blocker marks it resolved (history kept); deleting removes it (for mistakes).
- Blocked sessions get their own section in the TUI session list and are excluded from the tuios status line.

## Data model

New file `internal/session/sessionblocker.go`:

```go
type BlockerKind string

const (
	BlockerKindReview BlockerKind = "review"
	BlockerKindSlack  BlockerKind = "slack"
	BlockerKindOther  BlockerKind = "other"
)

type SessionBlocker struct {
	gorm.Model
	SessionID  uint        `json:"session_id" gorm:"index;not null"`
	Kind       BlockerKind `json:"kind"`
	Note       string      `json:"note,omitempty"`
	URL        string      `json:"url,omitempty"`
	ResolvedAt *time.Time  `json:"resolved_at,omitempty"`
}
```

- `Kind` is required and must be one of the three constants. `Note` and `URL` are optional.
- `SessionBlocker.Label()` returns `Note`, else `URL`, else `string(Kind)`.
- `SessionBlocker.IsOpen()` returns `ResolvedAt == nil`.

Changes to `Session` (`session.go`):

- `Blockers []SessionBlocker \`json:"blockers,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"\``
- `OpenBlockers() []SessionBlocker`: unresolved blockers, oldest first.
- `IsBlocked() bool`: `len(OpenBlockers()) > 0`.

`SessionStore` preloads `Blockers` wherever it already preloads `SetupSteps`. The session module registers `SessionBlocker` as a GORM model via its `ModelProvider`.

`SessionResponse` (`types.go`) gains `IsBlocked bool \`json:"is_blocked"\``, populated in `NewSessionResponse`.

## Store

New `SessionBlockerStore` (`sessionblockerstore.go`), following `SessionActionStore`:

- `Create(b *SessionBlocker) error`
- `GetByID(id uint) (*SessionBlocker, error)`
- `Update(b *SessionBlocker) error`
- `Delete(id uint) error`
- `ListBySessionID(sessionID uint, includeResolved bool) ([]SessionBlocker, error)`

`ErrBlockerNotFound = common.NewNotFound("blocker not found")`.

## Service

On `SessionService`:

- `AddBlocker(ctx, sessionID, AddBlockerInput{Kind, Note, URL}) (*Session, error)`. Fails with not-found if the session doesn't exist or is `deleted`. Returns the reloaded session.
- `ResolveBlocker(ctx, sessionID, blockerID) (*Session, error)`. Sets `ResolvedAt = now` if open; idempotent if already resolved. Fails with not-found if the blocker doesn't belong to the session.
- `DeleteBlocker(ctx, sessionID, blockerID) error`. Same ownership check.

No events are published; the TUI refetches sessions after each mutation.

## API

Added to `SessionRouter.Routes()`, handled by `SessionController`:

| Method | Path | Request | Response |
|---|---|---|---|
| `POST` | `/sessions/{id}/blockers` | `AddBlockerRequest{kind, note?, url?}` | `SessionResponse` |
| `PUT` | `/sessions/{id}/blockers/{blockerId}/resolve` | — | `SessionResponse` |
| `DELETE` | `/sessions/{id}/blockers/{blockerId}` | — | 204 |

`AddBlockerRequest.Bind` rejects a missing or unknown `kind` with 400.

Blockers are returned inline on `GET /sessions` and `GET /sessions/{id}`; there is no separate list endpoint.

## CLI

In `cmd/tui/sessions.go`, under `utena sessions`:

```
utena sessions block [name] --kind review|slack|other [--note "..."] [--url <url>]
utena sessions blockers [name] [--all]
utena sessions unblock [name] <blocker-id>
utena sessions unblock [name] --all
```

- `name` is the session name. When omitted, the session is resolved from `$UTENA_SESSION_ID`; if neither is available the command errors.
- `blockers` prints open blockers one per line: `id\tkind\tlabel\tage`. `--all` includes resolved ones with a resolved marker.
- `unblock --all` resolves every open blocker on the session.
- `sessions ls` adds a fourth column: `blocked` or empty.
- `status-line` skips sessions where `is_blocked` is true.

Session lookup by name reuses `fetchSessions` and matches on `Name`.

## TUI

### Session list (`internal/tui/sessionlist`)

- `rebuildFiltered` orders sessions into three stable groups: not hidden and not blocked, then blocked (not hidden), then hidden. Hidden takes precedence over blocked.
- `renderList` draws a `Blocked` title and divider (same style as the `Sessions` title) above the first blocked row, only when at least one blocked row is visible. The header is not a selectable row; cursor and offset math account for its two lines.
- Blocked rows show a muted `⏸ <kind>` tag in place of the Claude status badge; with more than one open blocker, `⏸ <kind of oldest> ×N`.
- New key `b` ("block") navigates to the blocker form for the selected session.

### Blocker form (new `internal/tui/blockerform`, new `router.BlockerFormView`)

- Kind selector cycling `review` / `slack` / `other` with `←/→`.
- `Note` and `URL` text inputs, both optional; `tab`/`shift+tab` moves focus.
- `enter` submits via `provider.AddBlocker`, then navigates back. `esc` cancels.
- A `blockerform.Start(sessionID)` message seeds the target session, mirroring `sessionprogress.Start`.

### Session detail (`internal/tui/sessiondetail`)

- New `Blockers` section listing open blockers as `kind · label · age`. Rendered in both the full detail view and the split-view side panel (`RenderPanel`).
- Full detail view only:
  - `j/k` moves a cursor over open blockers.
  - `u` resolves the highlighted blocker.
  - `x` deletes it, with two-press confirmation.
  - `b` opens the blocker form for this session.
- Resolved blockers are not shown in the TUI.

### Provider (`internal/tui/provider`)

`AddBlocker(sessionID, kind, note, url)`, `ResolveBlocker(sessionID, blockerID)`, `DeleteBlocker(sessionID, blockerID)` on the client, each refetching sessions on success like `ArchiveSession`.

## Testing

- `sessionblockerstore_test.go`: create, list with and without resolved, update, delete, cascade on session delete.
- `session` unit tests: `IsBlocked`, `OpenBlockers`, `Label` fallbacks.
- Service tests: add/resolve/delete, ownership check, resolve idempotency, `Status` unchanged by blocker operations.
- `sessionrouter_test.go`: the three endpoints, including 400 on invalid kind and 404 on foreign blocker.
- `sessionlist_test.go`: group ordering, header rendering, cursor never lands on the header.
- `sessiondetail/model_test.go`: blocker cursor and resolve/delete commands.
- `cmd/tui`: `ls` blocked column, status-line exclusion, name/env session resolution.

## Out of scope

- Any automatic creation or resolution of blockers (e.g. from PR review state or Slack).
- Editing an existing blocker.
- Showing resolved blocker history in the TUI.
