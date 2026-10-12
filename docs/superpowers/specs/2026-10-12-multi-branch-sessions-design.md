# Multi-branch sessions

## Goal

A session's worktree often moves through several branches: stacked PRs, or
merge-to-main then a fresh branch off main. Utena should track every branch a
session's worktrees have had checked out, so PRs on any of them are attributed
to the session and all branch-driven automation (PR notifications, activity
streaming, snapshot, dismiss, delete) covers them.

Detection must be automatic: no agent instructions, and it must catch branch
switches made by agents, by the user in a terminal, and by tools like gh-stack.

## Background

A session links to worktrees through `SessionWorktree`; each `Worktree` has a
single `BranchID` (`internal/git/worktree.go`), set when the session is created.
Every branch-driven flow keys off that ID:

- `sessionBranchIDs` (`internal/session/sessionservice.go:1464`) feeds
  `SyncPRActivity`, `SessionSnapshot` and `DismissSession`.
- `SessionStore.GetByBranchID` (`internal/session/sessionstore.go:231`) routes
  PR updates in `handlePRUpdated`, and gates auto-review session creation.
- `sessionBranchName` (`sessionservice.go:1674`) labels PR notifications.
- `ensureSessionWorktrees` / `RepairSession` recreate worktrees from `wt.Branch`.
- `cleanupSessionResources` deletes `wt.Branch` on delete-with-branch.

When the user runs `git checkout -b` inside a session worktree, none of this
changes. PRs on the new branch are not attributed to the session, and if the
user is a reviewer they can spawn a separate auto-review session.

Separately, `maybeCompleteSession` marks a session completed when any of its
PRs merges, which stops PR streaming. With multiple branches that is wrong
(merging the bottom of a stack, or merging then branching again).

## Decisions

- **Detection:** a global git config-based `post-checkout` hook (git ≥ 2.54)
  notifies the daemon, with reconciliation as a safety net.
- **Hook installation:** global `~/.gitconfig`, written by the dotfiles
  (shizuku) `utena` program.
- **Completion:** sessions are never auto-completed.
- **Delete with branch:** current branch force-deleted (`-D`, as today); past
  branches deleted only when that loses no work (see Consumers), otherwise kept
  with a warning. Plain `git branch -d` is not used: with squash merges it
  treats merged branches as unmerged.

### Why config-based hooks

Git 2.54 supports hooks declared in config:

```ini
[hook "utena"]
    command = utena hook post-checkout
    event = post-checkout
```

They run in addition to `.git/hooks/*` (the hookdir hook runs last), unlike
`core.hooksPath`, which replaces them and breaks pre-commit. They live in
config, not the `.bare/hooks` directory shared by every worktree. A repo can
opt out with `hook.utena.enabled = false`.

Rejected alternatives:

- **Daemon polling HEAD only:** works, but adds 15s-ish lag; kept only as the
  reconciliation fallback.
- **`utena session branch` command called by agents:** relies on agents
  remembering, and misses switches the user makes.

## Design

### 1. Data model: `SessionBranch`

New model in `internal/session/`, registered via the session module's
`Models()`:

```go
type SessionBranch struct {
	gorm.Model
	SessionID   uint        `gorm:"uniqueIndex:idx_session_branch;index;not null"`
	WorktreeID  uint        `gorm:"index;not null"`
	BranchID    uint        `gorm:"uniqueIndex:idx_session_branch;index;not null"`
	FirstSeenAt time.Time
	Branch      *git.Branch `gorm:"foreignKey:BranchID"`
}
```

`Session` gains `Branches []SessionBranch` (`foreignKey:SessionID`,
`OnDelete:CASCADE`), preloaded by the session store's `loaded()` scope.

New `SessionBranchStore` with `Add` (idempotent on the unique index).

A session's branches are the union of its worktrees' current `BranchID`s and
its `SessionBranch` rows (`Session.BranchIDs()`). The current branch therefore
never needs a history row; a row is written for the *old* branch at the moment
a worktree is repointed (and for the new one). No creation-time writes or
startup backfill are needed, and existing sessions work unchanged.

### 2. Detection

#### `SessionService.RecordWorktreeBranch(ctx, path, branchName)`

The single entry point. Given a worktree path and its checked-out branch:

1. Find the session worktree whose path matches, comparing
   `filepath.EvalSymlinks`-canonicalized paths on both sides, across all
   sessions in `SessionStore.List()` (any status but deleted, so an archived
   session that is later reactivated keeps its history). No match → return
   (not a utena worktree; the global hook fires everywhere).
2. Resolve the worktree's workspace (needed for the default-branch lookup and
   repo ID).
3. Skip if `branchName` is empty (detached HEAD, e.g. mid-rebase) or equals the
   repo's default branch (checking out `main` to pull must not adopt main's PRs).
4. Skip if it equals the worktree's current branch.
5. `FindOrCreateBranch(branchName, repoID)`.
6. Add `SessionBranch` rows for the old and the new branch (no-op if present).
7. Repoint `Worktree.BranchID` to the new branch so repair/recreate use the live
   branch. `Worktree.BranchID` is unique: if a stale row elsewhere still claims
   the branch, log a warning and skip the repoint; the history row is still
   written. (Git forbids one branch in two worktrees, so a live conflict can't
   occur.)
8. Replay: publish a `pull_request` notification to the session for each open PR
   on the new branch. PR sync may have seen the PR before the branch was
   attached, and its one-shot "new PR" event would otherwise never reach the
   session.

The default branch lookup reuses `gitCLI.defaultBranch`, exposed through a new
`GitService.DefaultBranch(ctx, repoPath)`.

#### Hook path

- New daemon endpoint `PUT /sessions/branch-hook`, body
  `{"path": "...", "branch": "..."}`, calling `RecordWorktreeBranch`. Always
  returns 204 for unknown paths.
- New CLI command `utena hook post-checkout <prev> <new> <flag>` in
  `cmd/tui/`. It ignores file checkouts (`flag != 1`), resolves
  `git rev-parse --show-toplevel` and `git branch --show-current` in the cwd,
  and sends the request with a ~1s timeout. It always exits 0 and prints
  nothing, so a stopped daemon never affects git.

`post-checkout` fires on `checkout`, `switch`, `checkout -b`, `worktree add`
and gh-stack's checkouts. Verified on git 2.54: config hooks receive the three
post-checkout arguments, and a sandboxed agent's process can reach
`localhost:3333`.

The daemon's own git invocations in `gitCLI` pass `-c hook.utena.enabled=false`,
so `worktree add` during session setup doesn't call back into the daemon.

#### Reconciliation fallback

`SessionService.SyncSessionBranches(ctx, sess)` runs `CurrentBranch` on each of
the session's worktree paths and calls `RecordWorktreeBranch`. It covers missed
events (daemon down), `git branch -m` renames, and tools that bypass the git
CLI. It runs:

- on daemon start, for live sessions;
- on `ActivateSession`;
- in a new `session.branches` job every 2 minutes, for sessions where
  `watchesPRActivity()` is true.

### 3. Consumers

| Consumer | Change |
|---|---|
| `sessionBranchIDs` | Replaced by `sess.BranchIDs()`. Drives `SyncPRActivity`, `SessionSnapshot`, `DismissSession`. |
| `SessionStore.GetByBranchID` | Match `worktrees.branch_id` OR `session_branches.branch_id`. Fixes attribution in `handlePRUpdated` and stops auto-review sessions spawning for the user's own stacked PRs. |
| `sessionBranchName` | Replaced by `s.prBranchName(pr)`, which uses `pr.HeadBranch` when loaded and otherwise `gitService.GetBranch(*pr.HeadBranchID)`. |
| `maybeCompleteSession`, `newlyMerged` | Removed. Merge notifications still reach the monitor. `StatusCompleted` stays so existing rows load; nothing sets it. |
| `cleanupSessionResources` | With `deleteBranch`: the current `wt.Branch` uses today's `CleanupBranch` (`-D`). Each other `SessionBranch` is deleted (`-D`) only if it has a merged PR whose head SHA equals the local branch tip (no commits added after the merge), or `git rev-list <branch> --not --remotes` is empty (every commit is on a remote). Otherwise it is kept and a warning logged. Failures (e.g. checked out elsewhere) are logged and skipped. |
| `ensureSessionWorktrees`, `RepairSession` | No code change; they follow the repointed `Worktree.BranchID`. |
| TUI session detail and list preview (`internal/tui/sessiondetail/model.go`, `internal/tui/sessionlist/sessionlist.go`) | PRs are filtered with `sess.FilterPRs` (all session branches) instead of the first worktree's branch. Detail shows the current branch per worktree as today, plus a "Past" line per past branch; PR lines already show number and state. |

Not changing:

- `UTENA_BRANCH` keeps the branch the session started on; only setup scripts
  read it, at creation time.
- No stack/base-branch modelling (`Branch.BaseBranchID` exists if needed later).

### 4. Dotfiles (`dotfiles/programs/utena/utena.go`)

The `git` program is in the server-safe base profile; `utena` is in `desktop`.
The hook goes in the `utena` program so machines without utena never get it.

In `Sync()`, run:

```sh
git config --global hook.utena.command "utena hook post-checkout"
git config --global --replace-all hook.utena.event post-checkout
```

The git CLI is used rather than the ini merge in `programs/git/git.go`, which
splits keys on the last dot and would write `[hook.utena]` instead of
`[hook "utena"]`. Both commands are idempotent. During implementation, confirm
the `git` program's ini.v1 re-merge of `~/.gitconfig` round-trips the
`[hook "utena"]` section unchanged on a second sync.

## Testing

- `SessionBranchStore`: add is idempotent; list by session; session IDs by
  branch.
- `RecordWorktreeBranch`:
  - unknown path is ignored;
  - a worktree with no live session is ignored;
  - detached HEAD and default branch are skipped;
  - a new branch writes history, repoints the worktree, and replays open PRs;
  - a unique-index collision on repoint still writes history.
- `Session.BranchIDs()` returns the union of current and history branches,
  deduplicated; `GetByBranchID` finds a session by either.
- `SyncSessionBranches` picks up a branch changed on disk (real git repo in a
  temp dir, as existing gitcli tests do).
- With a session that has two branches:
  - `handlePRUpdated` routes PRs on both;
  - `SyncPRActivity` and `SessionSnapshot` include both;
  - `DismissSession` dismisses PRs on both.
- A merged PR no longer changes session status.
- Delete with branch: current branch force-deleted; a past branch with a merged
  PR is deleted even though `-d` would refuse it; a past branch with
  local-only commits and no merged/closed PR survives, and the delete still
  completes.
- Daemon git calls don't trigger the utena hook.
- Router test for `PUT /sessions/branch-hook`.
- CLI: `utena hook post-checkout` exits 0 when the daemon is unreachable and
  ignores `flag=0`.
