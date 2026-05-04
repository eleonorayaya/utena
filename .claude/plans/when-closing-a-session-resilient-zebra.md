# Plan: Session Close/Start Fixes for Default Branch Worktrees

## Context

Two related bugs in how the system handles the **default branch** (e.g., `main`) worktree in bare repos:

1. **Close bug**: When a session is closed/archived/deleted, `CleanupBranch` unconditionally removes the worktree directory — even when the branch is the repo's default branch. For bare repos, the default branch worktree (e.g., `{bare-repo-parent}/main`) is a permanent checkout, not a disposable feature branch worktree.

2. **Start bug**: `CreateSession` has a hardcoded `branchName != "main"` guard that skips `createWorktree = true` for any branch named "main". For bare repos, this means starting a session on the main branch skips worktree setup entirely, so `worktreePath` is empty, and `setupTmux` falls back to `ws.Path` (the bare repo root dir, which has no working tree).

---

## Fix 1: Don't delete default-branch worktree on close

**File**: `internal/git/gitservice.go`  
**Function**: `CleanupBranch` (line 368)

Before calling `removeWorktree`, check if the branch is the repo's default branch. If so, skip both the filesystem removal and the DB record deletion (so future sessions can still find the worktree via `GetStartDir`).

```go
func (s *GitService) CleanupBranch(ctx context.Context, branch *Branch, repoPath string, deleteBranch bool) error {
    wt, err := s.worktreeStore.GetByBranchID(branch.ID)
    if err != nil && !errors.Is(err, ErrWorktreeNotFound) {
        return fmt.Errorf("failed to look up worktree: %w", err)
    }
    if wt != nil {
        if branch.Name != s.cli.defaultBranch(ctx, repoPath) {
            if err := s.cli.removeWorktree(ctx, repoPath, wt.Path); err != nil {
                return err
            }
            if err := s.worktreeStore.Delete(wt.ID); err != nil {
                return fmt.Errorf("failed to delete worktree record: %w", err)
            }
        }
    }
    if deleteBranch {
        if err := s.cli.deleteBranch(ctx, repoPath, branch.Name); err != nil {
            return err
        }
        branch.ExistsLocal = false
        return s.branchStore.Update(branch)
    }
    return nil
}
```

Note: `s.cli.defaultBranch` is already defined on `*gitCLI` (line 26 of `gitcli.go`) and `s.cli` is a direct `*gitCLI` field (not an interface), so this call requires no interface changes. If `defaultBranch` returns `""` (no remote configured), the condition stays false and worktrees are removed as before.

---

## Fix 2: Set cwd to worktree dir for all branches in bare repos

**File**: `internal/session/sessionservice.go`  
**Function**: `CreateSession` (line 152)

Change the hardcoded `branchName != "main"` to also allow worktree creation when the workspace is a bare repo (where all branches — including `main` — require a worktree):

```go
// Before
if ws != nil && ws.IsGitRepo && branchName != "" && baseBranchName == "" && branchName != "main" {
    createWorktree = true
}

// After
if ws != nil && ws.IsGitRepo && branchName != "" && baseBranchName == "" && (ws.IsBare || branchName != "main") {
    createWorktree = true
}
```

For bare repos, `SetupWorktree` is called, which checks if the worktree already exists on disk via `validateWorktree`. If it does (the common case for `main`), it calls `ensureWorktreeRecord` to create/refresh the DB record and returns the existing path — no git commands needed. `setupTmux` then receives the correct `worktreePath` and uses it as the tmux start directory.

For non-bare repos, behavior is unchanged: `main` does not create a separate worktree because the workspace root IS the main checkout.

---

## Critical Files

- `internal/git/gitservice.go` — Fix 1 (`CleanupBranch`, line 368)
- `internal/session/sessionservice.go` — Fix 2 (`CreateSession`, line 152)

---

## Verification

1. **Fix 1** (close): Create a session on the default branch (`main`) in a bare repo, close/archive it, verify the worktree directory still exists on disk.
2. **Fix 2** (start): Start a new session by selecting `main` as an existing branch in a bare repo workspace, verify the tmux session opens with cwd = the main worktree dir (e.g., `{bare-repo-parent}/main`), not the bare repo root.
3. Run `task test` to confirm no regressions.
