# Code Standards

## Repo layout

- One `cmd/<name>/` per program.
- `internal/` is flat, with one directory per module.
- One wiring package constructs everything. It's the only package that imports concrete implementations.
- Clients, such as a CLI or UI, talk to the daemon only through its API. They never import its internal packages.
- A separate product, such as a browser extension, gets its own top-level directory.

## Package structure

- One file per layer (model, store, service, controller, types), and only the layers the module needs. No empty layers.
- Request and response types live in one file per module.
- Exported fakes live in `testing.go`, next to the interface they fake.
- A large type's methods are split into files by feature within the package.

## Naming

- Names read well with the package name, so files and types aren't prefixed with it: `service.go` and `host.Service`, not `host.HostService`.
- Constructors are `NewX` and return a concrete pointer.
- Receivers are one letter, used consistently.
- HTTP handlers are verb plus noun: `ListSessions`.
- Fakes are `FakeX`.

## Wiring and lifecycle

- One function builds everything in dependency order.
- Every dependency, including stores and config, is passed to the constructor. No setters after construction, no functional options, no package-level variables used as test hooks.
- A constructor takes a few required arguments. Needing more than about four means the module does too much.
- Constructors never build their own collaborators.
- Modules pass each other concrete services. Interfaces exist only where there are real alternatives behind them.
- The wiring chooses implementations from config, and that's the only place the choice is made.
- One ordered list of modules sets the start order. Stop runs in reverse.
- Only a module that runs something has `Start` and `Stop`.

## Configuration

- One typed top-level config struct, loaded once at start and passed down. Nothing else reads config.
- Each module has its own config struct, embedded in the top-level struct as needed, and receives only its own.
- Every setting follows one precedence order: defaults, then the config file, then environment variables, then flags.
- Raw values are parsed into typed config, with defaults and validation, in one place at load time.
- One config file per install.

## HTTP routing

- chi. Each module mounts its routes under its own path.
- Global middleware handles request logging and panic recovery.
- Every route belongs to a module. None are defined in the wiring package.
- Each resource has its own controller with its own `Routes()`, and the module mounts its controllers. There are no separate router types.
- Routes are plain resources, plus named actions for anything beyond create, read, update and delete: `POST /sessions/{id}/start`.
- Anything that streams uses a websocket.

## Controllers

- HTTP handlers are thin: parse the request, authorize it, call the service, render the result.
- Controllers enforce authorization. Middleware identifies the caller and handles shared checks. Services trust their callers.
- Validation lives in the service, so the rules are the same over HTTP and from code. Controllers only check that a request is well formed.
- Path and query parameters are parsed with shared helpers.
- A controller calls only its own module's service and contains no business logic.

## Responses

- Responses have their own types, separate from database models, so a schema change doesn't change the API.
- One helper renders success and one renders errors.
- Lists are wrapped in a named key: `{"sessions": [...]}`.
- JSON keys are snake_case. Optional fields are pointers with `omitempty`.
- 201 for creates, 202 for work that continues in the background, 204 when there's nothing to return.
- Error bodies have one shape.

## Errors

- One app error type with categories (not found, invalid, conflict, unavailable), mapped to HTTP status in one place.
- Unexpected errors return a generic message. The details go to the log.
- Every not-found sentinel is an app error, so it always renders as a 404.
- Sentinel errors by default. A custom error type only when the error carries data.
- Messages are lowercase, with no "failed to", and always wrap with `%w`.
- Errors are returned, not logged and dropped. Only explicitly best-effort work logs and continues.

## Interfaces

- An interface exists only where there are real alternatives. It's defined in the package that owns the concept, and each implementation lives in its own package.
- No adapter structs bridging one module's service to another module's interface.
- A capability only some implementations have is its own small interface, checked by type assertion. It's never used to grow an interface.
- Each implementation has a compile-time check: `var _ runtime.Runtime = (*Driver)(nil)`.

## Communication between modules

- Modules call the modules below them directly. That's the default.
- Events are only for a lower module to tell an upper module that something changed, since the lower one can't call the upper one.
- Each event has a typed payload, defined in the package that publishes it.
- Event subscriptions are set up in the wiring, so every flow is visible in one place.
- Event handlers run on the publisher's goroutine and must return quickly. Slow work is handed off to the worker system.
- Publishing an event never fails the publisher. The event bus logs event handler errors.

## Database models

- Every model has a generated string ID and created and updated timestamps. Deletes are hard deletes.
- Statuses are string types with constants. Every status column has a default.
- No JSON columns without explicit approval for the specific case. Use columns or child tables instead.
- A model holds only stored fields. Computed values go in service or response types.
- Composite unique indexes are named. Child rows cascade-delete with their parent.
- A reference to another module's record is a plain ID field, with no GORM association across modules.
- Fields are required (`NOT NULL` with a default) unless "not set" means something different from zero. Nullable columns use `sql.Null[T]`.
- Updates don't depend on telling "not sent" apart from "set to null". Use named actions instead.

## Stores

- A store is a struct holding the database directly, built with `NewStore`, for one model.
- Store methods return an error. A lookup that finds nothing returns the module's not-found sentinel.
- Updates write only the columns they change.
- Upserts are a single `ON CONFLICT` statement.
- State changes are compare-and-set: update only when the row is in an expected status, then check whether a row changed.
- A store returns only its own model. There are no queries against another module's tables. Services compose across modules with batch lookups by ID.
- Unique violations are detected with GORM's translated errors.
- Stores use only portable SQL. Anything specific to one database lives in the database package.

## Transactions and migrations

- A write that spans several rows in one module runs in a transaction.
- Writes that span modules are made of steps that are safe to re-run, ordered so a partial failure is finished by running it again. There's no hand-written undo.
- Schema and data changes are versioned migrations, each applied once and recorded in a table. There's no AutoMigrate and no backfilling at startup.

## Files on disk

- A module that touches the filesystem takes a filesystem interface at construction. Tests use an in-memory one.
- A write that must not be left half-done writes to a temp file, syncs it, and renames it into place.
- Each file has one owning module, and everything else goes through that owner. There are no global locks around files.

## Domain logic and state

- Model methods may derive state from the model's own fields, with no I/O.
- Each status type has one transition table of allowed changes. Every status change goes through it.
- Status sets used as guards are named package-level values next to their status type.
- A rollup across statuses is one function with an explicit priority order.

## Services

- A long operation saves its record, returns, and finishes in the background. The record's status shows progress.
- On start, unfinished operations resume from their saved records. Every step is safe to re-run.
- Per-key locking uses one shared helper.
- In-memory service state is behind a mutex and only for what's fine to lose on restart. Anything that must survive goes in the database.
- "Already exists" and "already gone" count as success wherever retries are expected.

## Concurrency and background work

- Request-scoped work uses the request's context.
- All background work, one-off and periodic, goes through one central worker system. Periodic work is a named job. The worker system gives each job its own context, which is cancelled on shutdown and can carry a timeout, and shutdown waits for running jobs. Nothing else starts goroutines for background work.
- A job receives only an ID and loads what it needs.
- A job can be queued under a key. Queuing a key that's already waiting does nothing.
- Work that must stay in order for the same thing runs through a queue drained by a single worker.
- Each module's timeouts are in its config, with defaults.
- One clock interface is passed in at construction and faked in tests.
- Generation counters are used to ignore stale goroutines or connections after a reconnect.
- A slow streaming client is disconnected and re-syncs when it reconnects.

## External processes and systems

- External commands run through one shared command runner, which tests fake. Errors include the command's stderr, trimmed.
- Each client for an outside system lives in its own package, implementing the interface its domain owns. Only that package knows the wire protocol.
- Reconnects and retries use one shared helper with exponential backoff and a cap.
- After reconnecting to a stream, re-sync the full state. Don't assume no events were missed.
- Every call to an outside system has a timeout from config, applied by cancelling its context.
- List calls paginate until done, or stop at a stated limit.

## Logging

- `slog`, with structured key/value fields.
- Every log call that has a context passes it (`slog.InfoContext`), so request and job IDs come along automatically.
- One key per concept, defined as constants in one place: `session_id`, `host`.
- Levels:
  - error: something failed and nothing else will report it;
  - warn: something degraded but worked;
  - info: a state change;
  - debug: everything else.
- The worker system and the HTTP middleware log start, finish and duration for every job and request. Services don't.
- Log levels come from config. The database library's logging has its own level in config, like everything else.

## Tracing

- OpenTelemetry tracing throughout, even before anything collects it.
- HTTP requests, jobs, calls to outside systems and database queries each get a span, through standard instrumentation where it exists.
- The exporter comes from config. The default exports nothing.
- Log lines include the trace and span IDs.

## Tests

- testify everywhere: `require` for setup and preconditions, `assert` for checks.
- Tests are in the same package but use only what a caller would: constructors and exported methods.
- Names are `TestType_Behaviour`.
- A table test whenever cases differ only in inputs and outputs.
- Tests run in parallel by default (`t.Parallel()`).
- Each test gets its own fresh database from one shared test-database helper. Per-package setup returns a harness struct.
- Every interface has a hand-written fake that can inject errors and record calls.
- Async work is awaited with `Eventually`, or driven directly through the fake clock and worker system. Never bare sleeps.
- Shared test helpers live in one test-support package, not copied between packages.
- HTTP tests use `httptest` against the module's routes.

## Code style

- No code comments unless they're explicitly asked for.
- golangci-lint is the one tool for formatting and linting, configured in one `.golangci.yml`. It formats with gofumpt and goimports, and enforces rules it can check (allowed imports, banned calls) with built-in linters before any custom ones.
- Generics only for small, clearly reusable helpers, never for domain types.
- Anything that does I/O or can block takes a context as its first argument.

## Tooling

- Task is the only way to build, run, test, format and lint. Nobody runs `go` directly.
- `task test` runs with the race detector.
- `task fmt` and `task lint` both run through golangci-lint.
- CI runs `task lint` and `task test` on every PR, and both must pass.
- `task install` builds and installs the daemon.
- Dependency versions are pinned. Tools such as golangci-lint are pinned with `go tool` in `go.mod`, not installed globally.
- No per-package `CLAUDE.md` files. This doc and the specs are the guidance.
