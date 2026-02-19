# Git Worktree Integration Design

## Summary

Add git worktree support to session creation. When creating a session for a git-backed workspace, the user selects a base branch, and the daemon pulls latest and creates a worktree at `<workspace>/.worktrees/<session-name>` with a new branch named after the session.

## Requirements

1. New **branch picker view** in TUI after workspace selection (git repos only)
2. New **daemon endpoint** `GET /workspaces/{id}/branches` returns local branches
3. New **`internal/git/` module** with a `GitService` for raw git operations
4. Enhanced **session creation** — daemon pulls base branch and creates worktree
5. Worktree path passed to Zellij as the session's working directory

## Decisions

| Decision | Choice |
|----------|--------|
| Worktree location | `<workspace>/.worktrees/<session-name>` |
| Non-git workspaces | Skip branch picker, go straight to name input |
| Branch source | Local branches only (`git branch --format=%(refname:short)`) |
| Git executor | Daemon-side (git operations in session creation) |
| Code organization | `internal/git/` for raw git service, API exposed on workspace routes |
| Worktree branch | New branch from base (`git worktree add -b <session> ... <base>`) |

## Architecture

### New Module: `internal/git/gitservice.go`

Stateless service wrapping `os/exec` git commands. No store, no controller, no router, no module lifecycle.

Methods:
- `ListBranches(ctx, repoPath) ([]string, error)` — runs `git -C <path> branch --format=%(refname:short)`
- `Pull(ctx, repoPath, branch) error` — runs `git -C <path> pull origin <branch>`
- `CreateWorktree(ctx, repoPath, name, baseBranch) (string, error)` — runs `git -C <path> worktree add -b <name> .worktrees/<name> <baseBranch>`

### API Changes

New endpoint on workspace router:
- `GET /workspaces/{id}/branches` → `WorkspaceController.ListBranches` → `GitService.ListBranches`
- Response: `{"branches": ["main", "develop", "feature-x"]}`

### Session Model Changes

Two new optional fields on `Session`:
- `BaseBranch string` (`json:"base_branch,omitempty"`)
- `WorktreePath string` (`json:"worktree_path,omitempty"`)

### Session Creation Flow (daemon)

When `CreateSession` receives a session with `BaseBranch` set and the workspace is a git repo:
1. Pull latest from base branch (warn on failure, don't block)
2. Create worktree at `.worktrees/<session-name>` with new branch `-b <session-name>` from base
3. Store `WorktreePath` on session

### TUI Flow

```
Session List → [n] → Workspace Picker → [enter]
  → IF git repo: Branch Picker → [enter] → Name Input → [enter] → Create
  → ELSE:        Name Input → [enter] → Create
```

New view: `BranchPickerModel` using `bubbles/list`, same pattern as `NewSessionModel`.

New message types:
- `switchToBranchPickerMsg{workspace}` — transitions to branch picker
- `branchSelectedMsg{workspace, branch}` — branch chosen, go to name input
- `branchesLoadedMsg{branches}` — API response loaded

The `createSession` client function gains a `baseBranch` parameter. The `sessionCreatedMsg` gains a `worktreePath` field parsed from the API response. When present, `pendingWorkspacePath` is set to the worktree path so Zellij opens the session in the worktree directory.

## Data Flow

```
TUI                              Daemon
 |                                 |
 |-- GET /workspaces/{id}/branches |
 |   ← ["main", "develop"]        |
 |                                 |
 |-- POST /sessions ------------->|
 |   {id, workspace_id,           |-- git pull origin main
 |    base_branch: "main"}        |-- git worktree add -b my-session
 |                                 |   .worktrees/my-session main
 |   ← {worktree_path: "..."}     |
 |                                 |
 |-- zellij pipe create_session -->|
 |   (path = worktree path)       |
```

## Files

### New
| File | Purpose |
|------|---------|
| `internal/git/gitservice.go` | Git operations via os/exec |
| `internal/tui/branchpicker.go` | Branch picker TUI view |

### Modified
| File | Changes |
|------|---------|
| `internal/workspace/types.go` | Add `BranchListResponse` |
| `internal/workspace/workspacecontroller.go` | Add `gitService` field, `ListBranches` handler |
| `internal/workspace/workspacerouter.go` | Add `GET /{id}/branches` route |
| `internal/workspace/workspacemodule.go` | Create and export `GitService` |
| `internal/session/session.go` | Add `BaseBranch`, `WorktreePath` fields |
| `internal/session/sessionservice.go` | Add `gitService` dep, worktree logic in `CreateSession` |
| `internal/session/sessionmodule.go` | Pass `GitService` to session service |
| `internal/tui/app.go` | Add `branchPickerView`, transitions, `pendingBranch` |
| `internal/tui/newsession.go` | Conditional routing based on `IsGitRepo` |
| `internal/tui/client.go` | Add `fetchBranches`, modify `createSession` signature/response |
| `internal/session/sessionrouter_test.go` | Update `NewSessionService` constructor |
| `internal/session/sessionservice_test.go` | Update `NewSessionService` constructor |
| `internal/zellij/zellijservice_test.go` | Update `NewSessionService` constructor |
| `internal/workspace/workspacerouter_test.go` | Update `NewWorkspaceController` constructor |
