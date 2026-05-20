# Backend Patterns

Patterns for the daemon and its internal packages. TUI patterns are out of scope.

---

## 1. Module System

Every domain is packaged as a module. A module:

- Implements `common.Module`: `OnAppStart(ctx)`, `OnAppEnd(ctx)`, `Routes() chi.Router`
- Implements `common.ModelProvider` if it owns GORM models: `Models() []any`
- Exposes `RegisterJobs(jobs *jobs.JobService)` if it runs background tasks
- Bundles its own Store, Service, Controller, and Router — nothing else reaches in

`OnAppStart` is called in dependency order on startup. `OnAppEnd` is called in reverse order (LIFO) on shutdown. If a module has sub-components with their own lifecycle, it propagates the hooks down: Store → Service → etc.

See: `internal/session/sessionmodule.go`, `internal/api/app.go`

---

## 2. Manual Dependency Injection

There is no DI framework. `buildApp()` in `internal/api/app.go` constructs the full dependency graph explicitly. Construction order is: database first, then modules in dependency order.

Dependencies are passed through constructors — never via globals or package-level vars.

See: `internal/api/app.go`

---

## 3. Layered Domain Architecture

Each domain has four layers with single responsibilities:

| Layer | Responsibility |
|---|---|
| **Store** | Persistence only — reads and writes to the database. No business logic, no cross-domain calls. |
| **Service** | Business logic. Owns all state transitions. Publishes events. May call other domain services. |
| **Controller** | HTTP parsing and response formatting only. Thin — delegates immediately to the service. No business logic. |
| **Router** | Maps HTTP paths to controller methods. Nothing else. |

Event subscriptions are set up in `OnAppStart` on the service, not the module.

---

## 4. Standard Store Surface

Stores expose a small, stable interface. When a new query is needed, extend the filter struct — do not add a new method.

### Standard methods

```go
GetByID(id uint) (*T, error)
Add(entity *T) error
Update(entity *T) error
Delete(id uint) error
Find(filter TFilter) ([]T, error)
FindOne(filter TFilter) (*T, error)
```

`GetByID` is the only bespoke getter. Everything else goes through the filter.

### Filter structs

Each store defines its own filter type. Nil/zero fields are ignored.

```go
type SessionFilter struct {
    WorkspaceID     *uint
    BranchID        *uint
    TmuxSessionID   *uint
    ExcludeStatuses []SessionStatus
}
```

### What to avoid

- `GetByWorkspaceID`, `GetByBranchID`, `GetByPath` — finders for individual fields belong in the filter
- `SetHidden(id, bool)` — field-specific mutations belong in `Update`
- `DeleteByRepoID(repoID)` — scoped deletes belong in `Delete` with a filter

### Store implementation

All stores take `db.Database` (the interface), never `*gorm.DB` directly. This enables in-memory SQLite in tests without any mocking.

See: `internal/db/dbservice.go`, `internal/session/sessionstore.go`

---

## 5. Service Boundary Rule

**A model is only ever manipulated via public methods on its corresponding domain service.**

Services never hold a store from another domain. If domain A needs data from domain B, it calls domain B's service — which internally delegates to its own store.

```go
// Correct
s.gitService.GetBranch(branchID)

// Never
s.gitBranchStore.GetByID(branchID)
```

This applies without exception — including job tasks and background goroutines.

---

## 6. Service-to-Service Communication

Cross-domain data access goes through the owning service. Services may hold other services as constructor dependencies.

Current cross-service dependencies:
- `SessionService` → `GitService`, `WorkspaceService`, `TmuxService`
- `WorkspaceService` → `GitService`
- `TodoService` → `WorkspaceService`

When a direct dependency would create a cycle, use the event bus instead (see Pattern 7).

---

## 7. Event Bus

`eventbus.EventBus` is an in-memory synchronous pub/sub. Use it when a lower-level module needs to notify a higher-level module — a direction that would create a dependency cycle if done directly.

Handlers are called sequentially. The first error aborts the chain. There is no retry and no goroutine dispatch.

### When to use

- A lower module needs to notify a higher module (direct call would create a cycle)
- A module needs to react to a state change it doesn't own

When modules can depend on each other directly without creating a cycle, prefer direct calls.

### Defining events

Add a constant and payload struct to `internal/eventbus/events.go`:

```go
const ThingHappened = "module.thing_happened"

type ThingHappenedEvent struct {
    ID uint
}
```

### Publishing

Publish from the service layer only — never from controllers or stores:

```go
s.eventBus.Publish(ctx, eventbus.Event{
    Type: eventbus.ThingHappened,
    Data: eventbus.ThingHappenedEvent{ID: id},
})
```

### Subscribing

Subscribe in `OnAppStart`, handle in a private method:

```go
func (s *MyService) OnAppStart(ctx context.Context) error {
    s.eventBus.Subscribe(eventbus.ThingHappened, s.handleThingHappened)
    return nil
}

func (s *MyService) handleThingHappened(ctx context.Context, event eventbus.Event) error {
    data, ok := event.Data.(eventbus.ThingHappenedEvent)
    if !ok {
        return nil
    }
    // handle
    return nil
}
```

### Rules

- Events flow one way — if A publishes to B and B needs to call A, use a direct call for B→A, not a second event
- No circular event flows
- Controllers do not publish or subscribe

See: `internal/eventbus/eventbus.go`, `internal/eventbus/events.go`

---

## 8. Background Job Scheduler

Background tasks are registered with `JobService` and implement the `jobs.Job` interface:

```go
type Job interface {
    Name() string
    Interval() time.Duration
    Run(ctx context.Context) error
}
```

Each job runs in its own goroutine on a `time.Ticker`. Jobs can also be triggered manually via `TriggerJob(name)`, which sends a non-blocking signal on a buffered channel.

Job errors are logged but never fatal — the scheduler continues on the next tick. Jobs are registered in `RegisterJobs()` on the owning module, called during `buildApp()`.

See: `internal/jobs/jobservice.go`

---

## 9. CLI Subprocess Wrappers

External tools (git, tmux) are wrapped behind an interface with an `exec.CommandContext`-based implementation. The interface is the dependency — never the concrete struct.

This keeps the wrapper's internals private and allows mock injection in tests without modifying production code.

See: `internal/git/gitcli.go`, `internal/tmux/tmuxclient.go`

---

## 10. Domain Errors and HTTP Mapping

Errors are `common.AppError` values with a category:

| Category | HTTP status |
|---|---|
| `CategoryNotFound` | 404 |
| `CategoryInvalidRequest` | 400 |
| `CategoryConflict` | 409 |
| _(anything else)_ | 500 |

Controllers call `common.RenderError(w, r, err)` to map and render. Business logic constructs errors with `common.NewNotFound()`, `common.NewInvalidRequest()`, etc. Services define package-level sentinel errors (e.g. `ErrTodoNotFound`) for `errors.Is()` comparisons.

See: `internal/common/error.go`
