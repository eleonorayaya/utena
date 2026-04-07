# PR Sync Consolidation & Pending Session Design

## Summary

Consolidate `SyncRepoPRs` and `SyncAssignedPRs` into a single method, make PR discovery create pending sessions for assigned PRs, and auto-complete sessions when their PR is merged.

## Requirements

1. Single `SyncRepoPRs` method that also captures assignment status
2. PRs assigned to the current user auto-create `StatusPending` sessions
3. Existing sessions for a PR's branch are not duplicated
4. Merged PRs transition their session to `StatusCompleted`
5. Completed, unattached sessions are auto-archived after 5 minutes of inactivity
6. Remove `SyncAssignedPRs`, `ListAssignedPRs`, and related dead code

## Decisions

| Decision | Choice |
|----------|--------|
| Assignment detection | Current user login resolved at startup, compared against PR assignees list |
| Pending session scope | Only for PRs assigned to me, on repos linked to a workspace |
| Session-PR linkage | Implicit via shared `BranchID` — no new FK needed |
| Completed → archived threshold | 5 minutes since `LastUsedAt`, must not be attached |
| Cleanup job interval | 5 minutes |

## Changes

### 1. Model changes

**`RawPR`** — add `Assignees` field:
```go
Assignees []struct {
    Login string `json:"login"`
} `json:"assignees"`
```

**`PullRequest`** — add `IsAssignedToMe bool` field.

**`Session`** — add `StatusCompleted SessionStatus = "completed"`.

### 2. GitService: current user login

- Add `currentUser string` field to `GitService`
- `GitModule.OnAppStart` fetches `GetCurrentUser` and stores it on the service
- `syncRawPR` checks if `currentUser` is in the assignees list

### 3. Consolidate SyncRepoPRs

Extract `syncRawPR(ctx, raw, repo) error`:
- Upsert branch (create if unknown, mark `ExistsRemote`)
- Look up existing PR by repo+number
- Convert `RawPR` → `PullRequest` via `rawPRToPullRequest` (updated to set `IsAssignedToMe`)
- If existing: update, fire `EventPRStateChanged` if state changed
- If new: upsert, fire `EventPRDiscovered`

`SyncRepoPRs` becomes a loop calling `syncRawPR`.

Remove:
- `SyncAssignedPRs` from `GitService`
- `ListAssignedPRs` from `GitHubClient` interface and `githubRESTClient`
- Assigned PR call from `prSyncTask`
- `TestSyncAssignedPRs_*` tests

### 4. handlePRDiscovered → create pending session

When `EventPRDiscovered` fires:
- Check `IsAssignedToMe` — skip if false
- Check no session exists for this `BranchID` — skip if one does
- Check `DismissedPRStore` — skip if dismissed
- Look up workspace by `RepoID` from the event's `Repo`
- Create `StatusPending` session: name = branch name, workspace ID, branch ID, no tmux, no worktree

### 5. handlePRStateChanged → complete session

When `NewState == PRStateMerged`:
- Look up session by `HeadBranchID`
- If found and status is active/inactive/pending: set `StatusCompleted`

### 6. completedCleanupTask

- Registered in `SessionModule.RegisterJobs`
- Runs every 5 minutes
- Finds sessions where `Status == StatusCompleted && !IsAttached && LastUsedAt < 5 min ago`
- Calls `ArchiveSession` for each

### 7. ReconcileSession update

`StatusCompleted` should be treated like `StatusActive` — reconciliation still checks tmux/git health. Add it to the reconcile flow (don't skip it like `StatusPending`).

## Files touched

- `internal/git/githubclient.go` — add `Assignees` to `RawPR`, remove `ListAssignedPRs`
- `internal/git/pullrequest.go` — add `IsAssignedToMe` to `PullRequest`
- `internal/git/gitservice.go` — add `currentUser`, extract `syncRawPR`, remove `SyncAssignedPRs`
- `internal/git/gitmodule.go` — fetch current user on startup, remove assigned PR sync from task
- `internal/git/gitservice_pr_test.go` — update tests, remove assigned PR tests
- `internal/git/githubclient_test.go` — remove `ListAssignedPRs` test if present
- `internal/session/session.go` — add `StatusCompleted`
- `internal/session/sessionservice.go` — implement `handlePRDiscovered`, `handlePRStateChanged`
- `internal/session/sessionmodule.go` — register `completedCleanupTask`
- `internal/workspace/workspacestore.go` — may need `GetByRepoID` query
