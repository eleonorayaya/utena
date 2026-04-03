# GitHub PR Integration — Plan

Full technical specification: `docs/gh-integration-spec.md`
Task breakdown with dependencies: `docs/gh-integration-tasks.md`

## Summary

Restructure utena's data model and session lifecycle to support GitHub PR integration. Key changes:

1. **Git Module** (`internal/git/`): Four domain models (Repo, Branch, Worktree, PullRequest). Single public `GitService` facade. Internal gitCLI + GitHub GraphQL client (`shurcooL/githubv4`). No REST API — all triggered internally.

2. **Tmux Module** (`internal/tmux/`): New persisted `TmuxSession` model. Consolidated `TmuxService` (removes TmuxClient interface layer, gotmux called directly). Hooks update `IsAlive` state.

3. **Workspace**: `IsGitRepo bool` → `RepoID *uint` FK to git.Repo. Depends on git module.

4. **Session State Machine**: 7 states — `pending`, `creating`, `active`, `inactive`, `broken`, `archived`, `deleted`. All activation is async (202 Accepted). Tmux death → inactive (not broken). Broken reserved for genuine git inconsistencies. No Resources struct — state derived from associated models.

5. **PRs via Branch**: No session-PR join table. PRs loaded through `Session → Branch → PullRequests`. Dismissed PRs tracked in session module.

6. **Background Sync** (`internal/sync/`): Generic `SyncManager` + `SyncTask` interface. Git registers PR/branch sync. Session registers reconciliation. No REST API.

7. **Signal System**: `Signal` type in `internal/common/`. Each model has `Signals()` method. Session controller aggregates. No cross-module session dependency.

## Latest Changes (from review feedback)

- **Consolidated TmuxService**: `TmuxClient` interface removed. `TmuxService` calls gotmux directly + manages DB persistence.
- **Always-async activation**: All `ActivateSession` calls return 202 and run setup in background goroutine, regardless of source state (pending, inactive, or active).
- **Module-level dependencies**: Modules accept other modules in init, extract services internally.

---

The spec and tasks docs below contain all implementation details. This plan file is the summary.

See `docs/gh-integration-spec.md` for:
- Full model definitions (Repo, Branch, Worktree, PullRequest, TmuxSession, Session)
- GitService public API (facade methods)
- GitHub client design
- Consolidated TmuxService design
- Session state machine with transition table
- Async activation flow
- Signal system design
- Background sync system
- API endpoint specifications
- Edge cases

See `docs/gh-integration-tasks.md` for:
- 25 tasks across 4 phases
- Dependency graph enabling maximum parallelism (up to 6 tasks simultaneously)
- Exhaustive test coverage per task
- Implementation order

The full spec is in `docs/gh-integration-spec.md` and the task breakdown is in `docs/gh-integration-tasks.md`.
