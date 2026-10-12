# Multi-branch Sessions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Track every branch a session's worktrees have had checked out, so PRs on any of them are attributed to the session and all branch-driven automation covers them.

**Architecture:** A global git config-based `post-checkout` hook runs `utena hook post-checkout`, which reports `{path, branch}` to a new daemon endpoint. The daemon matches the path to a session worktree, records the old and new branch in a `SessionBranch` history table, and repoints the worktree. A session's branches are its worktrees' current branches plus that history. A reconciliation pass (daemon start, activation, 2-minute job) catches anything the hook missed.

**Tech Stack:** Go 1.25, GORM + SQLite, chi, cobra, Bubbletea; git ≥ 2.54 (config-based hooks). Dotfiles: shizuku (Go).

**Spec:** `docs/superpowers/specs/2026-10-12-multi-branch-sessions-design.md`

**Repos (two):**
- utena (Tasks 1–8)
- dotfiles / shizuku (Task 9)

## Global Constraints

- Build/test only through Task: utena `task test` / `task fmt` from the utena root; dotfiles `task test` / `task lint` from the dotfiles root. Never run `go test` / `go build` directly. Never `cd` into subdirectories to run commands.
- No code comments (both repos' CLAUDE.md forbid them unless asked).
- `post-checkout` arguments: `<prev-HEAD> <new-HEAD> <flag>`; `flag == "1"` is a branch checkout. Verified on git 2.54 that config hooks receive all three.
- Hook config is exactly: `hook.utena.command = utena hook post-checkout`, `hook.utena.event = post-checkout`.
- Daemon git invocations must pass `-c hook.utena.enabled=false`.
- Daemon endpoint: `PUT /sessions/branch-hook`, body `{"path": "...", "branch": "..."}`, 204 on success including unknown paths.
- The hook CLI always exits 0, prints nothing, and uses a 1s HTTP timeout.
- Sessions are never auto-completed on PR merge.
- Past branches on delete-with-branch: deleted (`-D`) only if the branch has a merged PR whose HeadSHA equals the local branch tip (ruling during execution) or `git rev-list refs/heads/<branch> --not --remotes` is empty; otherwise kept with a warning.

## Review Focus

- **Symlinked paths:** a worktree stored as `/var/folders/...` but reported by `git rev-parse --show-toplevel` as `/private/var/folders/...` must still match. Test in Task 4 (`TestRecordWorktreeBranch_MatchesCanonicalPath`).
- **Detached HEAD mid-rebase:** an empty branch name must write no history and leave the worktree pointing at its branch. Test in Task 4 (`TestRecordWorktreeBranch_SkipsDetachedAndDefault`).
- **Checking out `main` inside a session:** must not adopt main or its PRs. Test in Task 4 (same test).
- **Switching back to an earlier branch (A→B→A):** no duplicate history row, no unique-index error, worktree points back at A. Test in Task 4 (`TestRecordWorktreeBranch_SwitchBackIsIdempotent`).
- **Daemon not running when the hook fires:** `git checkout` must be unaffected and the CLI must return quickly with no output. Test in Task 6 (`TestReportCheckout_UnreachableDaemon`).

---

### Task 1: Git layer — hook-safe git invocations and new helpers

**Files:**
- Modify: `internal/git/gitcli.go` (all `exec.CommandContext(ctx, "git", ...)` call sites; add `gitCommand`, `hasUnpushedCommits`)
- Modify: `internal/git/gitservice.go` (add `DefaultBranch`, `HasUnpushedCommits`, `RepointWorktree`)
- Test: `internal/git/gitcli_test.go`, `internal/git/gitservice_branch_test.go` (create)

**Interfaces:**
- Produces:
  - `func gitCommand(ctx context.Context, args ...string) *exec.Cmd` (package-private)
  - `func (s *GitService) DefaultBranch(ctx context.Context, repoPath string) string`
  - `func (s *GitService) HasUnpushedCommits(ctx context.Context, repoPath string, branch string) (bool, error)`
  - `func (s *GitService) RepointWorktree(wt *Worktree, branch *Branch) error`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/gitcli_test.go`:

```go
func TestGitCLI_CreateWorktree_DoesNotRunUtenaHook(t *testing.T) {
	repo := initTestRepo(t)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	run(t, repo, "git", "config", "hook.utena.command", "touch "+marker)
	run(t, repo, "git", "config", "hook.utena.event", "post-checkout")

	svc := newGitCLI()
	_, err := svc.createWorktree(context.Background(), repo, "eqt/no-hook", "main", "")
	require.NoError(t, err)

	_, err = os.Stat(marker)
	require.True(t, os.IsNotExist(err), "daemon git calls must not trigger the utena hook")
}

func TestGitCLI_HasUnpushedCommits(t *testing.T) {
	clone, _ := initTestRepoWithOrigin(t)
	svc := newGitCLI()
	ctx := context.Background()

	run(t, clone, "git", "branch", "pushed-only")
	unpushed, err := svc.hasUnpushedCommits(ctx, clone, "pushed-only")
	require.NoError(t, err)
	require.False(t, unpushed)

	run(t, clone, "git", "checkout", "-b", "local-work")
	run(t, clone, "git", "commit", "--allow-empty", "-m", "wip")
	unpushed, err = svc.hasUnpushedCommits(ctx, clone, "local-work")
	require.NoError(t, err)
	require.True(t, unpushed)

	_, err = svc.hasUnpushedCommits(ctx, clone, "does-not-exist")
	require.Error(t, err)
}
```

Create `internal/git/gitservice_branch_test.go`:

```go
package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func TestGitService_DefaultBranch(t *testing.T) {
	clone, _ := initTestRepoWithOrigin(t)
	svc := NewGitService(testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{}))
	require.Equal(t, "main", svc.DefaultBranch(context.Background(), clone))
}

func TestGitService_RepointWorktree(t *testing.T) {
	database := testdb.New(t, &Repo{}, &Branch{}, &Worktree{}, &PullRequest{})
	svc := NewGitService(database)

	repo := &Repo{Path: "/tmp/repo", FullName: "o/r"}
	require.NoError(t, database.Create(repo).Error)
	oldBranch := &Branch{Name: "old", RepoID: repo.ID}
	newBranch := &Branch{Name: "new", RepoID: repo.ID}
	require.NoError(t, database.Create(oldBranch).Error)
	require.NoError(t, database.Create(newBranch).Error)
	wt := &Worktree{Path: "/tmp/wt", BranchID: oldBranch.ID, RepoID: repo.ID, Status: WorktreeStatusPresent}
	require.NoError(t, database.Create(wt).Error)

	loaded, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	loaded.Branch = oldBranch

	require.NoError(t, svc.RepointWorktree(loaded, newBranch))

	reloaded, err := svc.worktreeStore.GetByID(wt.ID)
	require.NoError(t, err)
	require.Equal(t, newBranch.ID, reloaded.BranchID)
}
```

`loaded.Branch = oldBranch` matters: GORM's `Save` rewrites a belongs-to foreign key from a loaded association, so `RepointWorktree` must replace `wt.Branch` too.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `svc.hasUnpushedCommits undefined`, `svc.DefaultBranch undefined`, `svc.RepointWorktree undefined`. (The hook test may also fail because the marker file is created.)

- [ ] **Step 3: Implement**

In `internal/git/gitcli.go`, add after `newGitCLI`:

```go
func gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "git", append([]string{"-c", "hook.utena.enabled=false"}, args...)...)
}
```

Then replace every `exec.CommandContext(ctx, "git", ` in `internal/git/gitcli.go` with `gitCommand(ctx, ` (Edit with `replace_all: true`). There are 21 sites, including `runBareClone` and the `fallback` pull command. Confirm none remain:

Run: `grep -n 'exec.CommandContext(ctx, "git"' internal/git/gitcli.go`
Expected: no output.

Add to `internal/git/gitcli.go` after `currentBranch`:

```go
func (s *gitCLI) hasUnpushedCommits(ctx context.Context, repoPath string, branch string) (bool, error) {
	cmd := gitCommand(ctx, "-C", repoPath, "rev-list", "--max-count=1", "refs/heads/"+branch, "--not", "--remotes")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("failed to list unpushed commits for %s: %w", branch, err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}
```

Add to `internal/git/gitservice.go` after `CurrentBranch`:

```go
func (s *GitService) DefaultBranch(ctx context.Context, repoPath string) string {
	return s.cli.defaultBranch(ctx, repoPath)
}

func (s *GitService) HasUnpushedCommits(ctx context.Context, repoPath string, branch string) (bool, error) {
	return s.cli.hasUnpushedCommits(ctx, repoPath, branch)
}

func (s *GitService) RepointWorktree(wt *Worktree, branch *Branch) error {
	if wt == nil || branch == nil {
		return fmt.Errorf("worktree and branch are required")
	}
	wt.BranchID = branch.ID
	wt.Branch = branch
	return s.worktreeStore.Update(wt)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS (whole suite, including existing git tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/gitcli.go internal/git/gitservice.go internal/git/gitcli_test.go internal/git/gitservice_branch_test.go
git commit -m "feat(git): disable utena hook for daemon git calls; add branch helpers"
```

---

### Task 2: `SessionBranch` model, store, and session branch set

**Files:**
- Create: `internal/session/sessionbranch.go`, `internal/session/sessionbranchstore.go`
- Modify: `internal/session/session.go` (add `Branches` field)
- Modify: `internal/session/sessionstore.go:24-37` (`loaded()` preloads), `:231-248` (`GetByBranchID`)
- Modify: `internal/session/sessionmodule.go:68-70` (`Models()`)
- Modify: `internal/session/sessionservice.go:68-100` (service field)
- Modify test DB model lists: `internal/session/sessionstore_test.go:22` and `:370`, `internal/session/sessionworktreestore_test.go:~21`, `internal/session/sessionservice_pr_test.go:~65`
- Test: `internal/session/sessionbranch_test.go` (create)

**Interfaces:**
- Produces:
  - `type SessionBranch struct { gorm.Model; SessionID uint; WorktreeID uint; BranchID uint; FirstSeenAt time.Time; Branch *git.Branch }`
  - `Session.Branches []SessionBranch` (JSON `branches`)
  - `func (s *Session) BranchIDs() []uint` — current worktree branches first, then history, deduplicated, zero IDs skipped
  - `func (s *Session) PastBranches() []git.Branch` — history branches not currently checked out in any of the session's worktrees (requires `Branch` preloaded)
  - `func (s *Session) FilterPRs(prs []git.PullRequest) []git.PullRequest`
  - `type SessionBranchStore`, `NewSessionBranchStore(db.Database)`, `func (s *SessionBranchStore) Add(sb *SessionBranch) error` (no-op on duplicate `(session_id, branch_id)`)
  - `SessionService.sessionBranchStore *SessionBranchStore`
  - `SessionStore.GetByBranchID` matches the current worktree branch OR history

- [ ] **Step 1: Add `&SessionBranch{}` to every session test DB**

In each `testdb.New(...)` call listed above, add `&SessionBranch{},` immediately after `&SessionWorktree{},`. (Without this, every preload of `Branches` fails with "no such table".)

- [ ] **Step 2: Write the failing tests**

Create `internal/session/sessionbranch_test.go`:

```go
package session

import (
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/require"
)

func TestSession_BranchIDs_UnionsCurrentAndHistory(t *testing.T) {
	sess := Session{
		Worktrees: []SessionWorktree{
			{Worktree: &git.Worktree{BranchID: 3}},
			{Worktree: &git.Worktree{BranchID: 0}},
			{Worktree: nil},
		},
		Branches: []SessionBranch{{BranchID: 1}, {BranchID: 3}, {BranchID: 2}},
	}
	require.Equal(t, []uint{3, 1, 2}, sess.BranchIDs())
}

func TestSession_PastBranches_ExcludesCurrent(t *testing.T) {
	sess := Session{
		Worktrees: []SessionWorktree{{Worktree: &git.Worktree{BranchID: 2}}},
		Branches: []SessionBranch{
			{BranchID: 1, Branch: &git.Branch{Name: "first"}},
			{BranchID: 2, Branch: &git.Branch{Name: "current"}},
			{BranchID: 4},
		},
	}
	past := sess.PastBranches()
	require.Len(t, past, 1)
	require.Equal(t, "first", past[0].Name)
}

func TestSession_FilterPRs(t *testing.T) {
	one, two, other := uint(1), uint(2), uint(9)
	sess := Session{
		Worktrees: []SessionWorktree{{Worktree: &git.Worktree{BranchID: 2}}},
		Branches:  []SessionBranch{{BranchID: 1}},
	}
	prs := []git.PullRequest{
		{Number: 10, HeadBranchID: &one},
		{Number: 20, HeadBranchID: &two},
		{Number: 30, HeadBranchID: &other},
		{Number: 40},
	}
	got := sess.FilterPRs(prs)
	require.Len(t, got, 2)
	require.Equal(t, 10, got[0].Number)
	require.Equal(t, 20, got[1].Number)
}

func TestSessionBranchStore_AddIsIdempotent(t *testing.T) {
	database := setupTestDB(t)
	store := NewSessionBranchStore(database)
	sess := &Session{Name: "s", Status: StatusActive}
	require.NoError(t, database.Create(sess).Error)
	branch := &git.Branch{Name: "b", RepoID: 1}
	require.NoError(t, database.Create(branch).Error)

	first := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: branch.ID, FirstSeenAt: first}))
	require.NoError(t, store.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: branch.ID, FirstSeenAt: time.Now()}))

	var rows []SessionBranch
	require.NoError(t, database.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.True(t, rows[0].FirstSeenAt.Equal(first), "first-seen time must be preserved")
}

func TestSessionStore_LoadsBranchesAndFindsSessionByHistoryBranch(t *testing.T) {
	database, wsID, branch, _ := setupTestDBWithGitAndTmux(t)
	store := NewSessionStore(database)
	swtStore := NewSessionWorktreeStore(database)

	sess := &Session{Name: "multi", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, store, swtStore, sess, wsID)

	past := &git.Branch{Name: "past-branch", RepoID: branch.RepoID}
	require.NoError(t, database.Create(past).Error)
	require.NoError(t, NewSessionBranchStore(database).Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: past.ID, FirstSeenAt: time.Now()}))

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Len(t, loaded.Branches, 1)
	require.NotNil(t, loaded.Branches[0].Branch)
	require.Equal(t, "past-branch", loaded.Branches[0].Branch.Name)

	found, err := store.GetByBranchID(past.ID)
	require.NoError(t, err)
	require.Equal(t, sess.ID, found.ID)
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: SessionBranch`, `undefined: NewSessionBranchStore`, `sess.BranchIDs undefined`.

- [ ] **Step 4: Implement**

Create `internal/session/sessionbranch.go`:

```go
package session

import (
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"gorm.io/gorm"
)

type SessionBranch struct {
	gorm.Model
	SessionID   uint        `json:"session_id" gorm:"uniqueIndex:idx_session_branch;index;not null"`
	WorktreeID  uint        `json:"worktree_id" gorm:"index;not null"`
	BranchID    uint        `json:"branch_id" gorm:"uniqueIndex:idx_session_branch;index;not null"`
	FirstSeenAt time.Time   `json:"first_seen_at"`
	Branch      *git.Branch `json:"branch,omitempty" gorm:"foreignKey:BranchID;constraint:OnDelete:CASCADE"`
}

func (s *Session) currentBranchIDs() map[uint]struct{} {
	current := make(map[uint]struct{}, len(s.Worktrees))
	for i := range s.Worktrees {
		if wt := s.Worktrees[i].Worktree; wt != nil && wt.BranchID != 0 {
			current[wt.BranchID] = struct{}{}
		}
	}
	return current
}

func (s *Session) BranchIDs() []uint {
	seen := make(map[uint]struct{})
	out := make([]uint, 0, len(s.Worktrees)+len(s.Branches))
	add := func(id uint) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	for i := range s.Worktrees {
		if wt := s.Worktrees[i].Worktree; wt != nil {
			add(wt.BranchID)
		}
	}
	for i := range s.Branches {
		add(s.Branches[i].BranchID)
	}
	return out
}

func (s *Session) PastBranches() []git.Branch {
	current := s.currentBranchIDs()
	var out []git.Branch
	for i := range s.Branches {
		sb := s.Branches[i]
		if sb.Branch == nil {
			continue
		}
		if _, ok := current[sb.BranchID]; ok {
			continue
		}
		out = append(out, *sb.Branch)
	}
	return out
}

func (s *Session) FilterPRs(prs []git.PullRequest) []git.PullRequest {
	owned := make(map[uint]struct{})
	for _, id := range s.BranchIDs() {
		owned[id] = struct{}{}
	}
	var out []git.PullRequest
	for _, pr := range prs {
		if pr.HeadBranchID == nil {
			continue
		}
		if _, ok := owned[*pr.HeadBranchID]; ok {
			out = append(out, pr)
		}
	}
	return out
}
```

Create `internal/session/sessionbranchstore.go`:

```go
package session

import (
	"errors"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm/clause"
)

type SessionBranchStore struct {
	db db.Database
}

func NewSessionBranchStore(database db.Database) *SessionBranchStore {
	return &SessionBranchStore{db: database}
}

func (s *SessionBranchStore) Add(sb *SessionBranch) error {
	if sb == nil {
		return errors.New("session branch cannot be nil")
	}
	return s.db.Omit("Branch").Clauses(clause.OnConflict{DoNothing: true}).Create(sb).Error
}
```

In `internal/session/session.go`, add to `Session` after the `Worktrees` field:

```go
	Branches       []SessionBranch        `json:"branches,omitempty" gorm:"foreignKey:SessionID;constraint:OnDelete:CASCADE"`
```

In `internal/session/sessionstore.go` `loaded()`, append after `Preload("Worktrees.Worktree.Branch")`:

```go
		Preload("Worktrees.Worktree.Branch").
		Preload("Branches").
		Preload("Branches.Branch")
```

(Move the closing of the chain accordingly — the old last line loses nothing, it just gains a trailing `.`.)

Replace `GetByBranchID` in `internal/session/sessionstore.go`:

```go
func (s *SessionStore) GetByBranchID(branchID uint) (*Session, error) {
	viaWorktree := s.db.
		Model(&SessionWorktree{}).
		Select("session_worktrees.session_id").
		Joins("JOIN worktrees ON worktrees.id = session_worktrees.worktree_id").
		Where("worktrees.branch_id = ?", branchID)
	viaHistory := s.db.
		Model(&SessionBranch{}).
		Select("session_branches.session_id").
		Where("session_branches.branch_id = ?", branchID)
	var session Session
	if err := s.loaded().First(&session, "sessions.id IN (?) OR sessions.id IN (?)", viaWorktree, viaHistory).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := s.enrichWorkspaces([]*Session{&session}); err != nil {
		return nil, err
	}
	return &session, nil
}
```

In `internal/session/sessionmodule.go` `Models()`, add `&SessionBranch{}` after `&SessionWorktree{}`:

```go
	return []any{&Session{}, &SessionWorktree{}, &SessionBranch{}, &DismissedPR{}, &SessionAction{}, &SessionSetupStep{}}
```

In `internal/session/sessionservice.go`, add a field `sessionBranchStore *SessionBranchStore` to `SessionService` (after `sessionWorktreeStore`) and in `NewSessionService` set `sessionBranchStore: NewSessionBranchStore(store.db),`. (Constructing it from the session store's DB keeps the `NewSessionService` signature, which ~6 test helpers call, unchanged.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `task test`
Expected: PASS (new tests and the whole existing suite).

- [ ] **Step 6: Commit**

```bash
git add internal/session/
git commit -m "feat(session): add SessionBranch history and session branch set"
```

---

### Task 3: Attribution consumers and removing auto-completion

**Files:**
- Modify: `internal/session/sessionservice.go` — `DismissSession` (~1387), `handlePRUpdated` (~1549), `notifyPRUpdated` (~1583), `SessionSnapshot` (~1588), `SyncPRActivity` (~1614); delete `sessionBranchIDs` (~1464), `sessionBranchName` (~1674), `maybeCompleteSession` (~1722)
- Modify: `internal/session/sessionservice_pr_test.go:259-291` (replace the merge-completes test)
- Test: `internal/session/sessionservice_monitor_test.go`, `internal/session/sessionservice_pr_test.go`

**Interfaces:**
- Consumes: `Session.BranchIDs()`, `SessionBranchStore.Add` (Task 2)
- Produces: `func (s *SessionService) prBranchName(pr *git.PullRequest) string`

- [ ] **Step 1: Write the failing tests**

Add to `internal/session/sessionservice_monitor_test.go` a helper and tests:

```go
func (env *prTestEnv) addHistoryBranch(t *testing.T, sessionID uint, name string) *git.Branch {
	t.Helper()
	branch := &git.Branch{Name: name, RepoID: env.repo.ID}
	require.NoError(t, env.database.Create(branch).Error)
	require.NoError(t, NewSessionBranchStore(env.database).Add(&SessionBranch{SessionID: sessionID, WorktreeID: 1, BranchID: branch.ID, FirstSeenAt: time.Now()}))
	return branch
}

func TestHandlePRUpdated_RoutesPRFromHistoryBranch(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()
	got := collectNotifications(env)

	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	pastID := past.ID

	require.NoError(t, env.service.handlePRUpdated(ctx, eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{Number: 5, HeadBranchID: &pastID, Title: "Base", State: git.PRStateOpen},
			Previous:    &git.PullRequest{State: git.PRStateDraft},
			Repo:        env.repo,
		},
	}))

	require.Len(t, got(), 1)
	require.Equal(t, sess.ID, got()[0].SessionID)
	require.Equal(t, "stacked-base", got()[0].Data.(git.PRNotification).Branch)
}

func TestSessionSnapshot_IncludesHistoryBranchPRs(t *testing.T) {
	env := setupPRTestEnv(t)
	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	currentID, pastID := env.branch.ID, past.ID
	require.NoError(t, env.database.Create(&git.PullRequest{RepoID: env.repo.ID, Number: 1, HeadBranchID: &currentID, State: git.PRStateOpen}).Error)
	require.NoError(t, env.database.Create(&git.PullRequest{RepoID: env.repo.ID, Number: 2, HeadBranchID: &pastID, State: git.PRStateOpen}).Error)

	snapshot := env.service.SessionSnapshot(context.Background(), sess.ID)

	require.Len(t, snapshot, 2)
	branches := []string{snapshot[0].Data.(git.PRNotification).Branch, snapshot[1].Data.(git.PRNotification).Branch}
	require.ElementsMatch(t, []string{"feature-pr", "stacked-base"}, branches)
}

func TestSyncPRActivity_CoversHistoryBranches(t *testing.T) {
	client := &git.MockGitHubClient{Reviews: []*github.PullRequestReview{{
		ID:    github.Ptr(int64(101)),
		State: github.Ptr("APPROVED"),
		User:  &github.User{Login: github.Ptr("reviewer"), Type: github.Ptr("User")},
	}}}
	env := setupPRTestEnv(t, git.WithGitHubClient(client))
	got := collectNotifications(env)

	sess := &Session{Name: "stacked", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := env.addHistoryBranch(t, sess.ID, "stacked-base")
	pastID := past.ID
	require.NoError(t, env.database.Create(&git.PullRequest{
		RepoID: env.repo.ID, Number: 2, HeadBranchID: &pastID, State: git.PRStateOpen,
		HeadSHA: "sha1", ActivityBaselined: true, LastReviewID: 100,
	}).Error)

	require.NoError(t, env.service.SyncPRActivity(context.Background()))

	require.Len(t, got(), 1)
	require.Equal(t, sess.ID, got()[0].SessionID)
	require.Equal(t, git.NotificationReview, got()[0].Type)
}
```

Add to `internal/session/sessionservice_pr_test.go`:

```go
func TestDismissSession_DismissesHistoryBranchPRs(t *testing.T) {
	env := setupPRTestEnv(t)
	sess := &Session{Name: "pending", Status: StatusPending, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, env.branch.ID, 0)
	past := &git.Branch{Name: "older", RepoID: env.repo.ID}
	require.NoError(t, env.database.Create(past).Error)
	require.NoError(t, NewSessionBranchStore(env.database).Add(&SessionBranch{SessionID: sess.ID, WorktreeID: 1, BranchID: past.ID, FirstSeenAt: time.Now()}))
	pastID := past.ID
	pr := &git.PullRequest{RepoID: env.repo.ID, Number: 77, HeadBranchID: &pastID, State: git.PRStateOpen}
	require.NoError(t, env.database.Create(pr).Error)

	require.NoError(t, env.service.DismissSession(context.Background(), sess.ID))

	require.True(t, env.dismissedPRStore.IsDismissed(pr.ID))
}
```

Replace `TestHandlePRUpdated_CompletesSessionOnMerge` (lines ~259-291) with:

```go
func TestHandlePRUpdated_MergeDoesNotCompleteSession(t *testing.T) {
	env := setupPRTestEnv(t)
	ctx := context.Background()
	got := collectNotifications(env)

	branchID := env.branch.ID
	sess := &Session{Name: "feature-pr", Status: StatusActive, LastUsedAt: time.Now()}
	require.NoError(t, env.sessionStore.Add(sess))
	env.attachBranchWorktree(t, sess.ID, branchID, 0)

	require.NoError(t, env.service.handlePRUpdated(ctx, eventbus.Event{
		Type: git.EventPRUpdated,
		Data: git.PRUpdatedEvent{
			PullRequest: &git.PullRequest{Number: 42, HeadBranchID: &branchID, State: git.PRStateMerged},
			Previous:    &git.PullRequest{State: git.PRStateOpen},
			Repo:        env.repo,
		},
	}))

	updated, err := env.sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, updated.Status)
	require.Len(t, got(), 1, "the merge is still reported to the session")
}
```

(`sessionservice_monitor_test.go` already imports `github`; `sessionservice_pr_test.go` already imports `eventbus`, `git`, `time`, `context`.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — the new history tests get no notifications / 1 snapshot entry, `IsDismissed` is false, and the merge test sees `StatusCompleted`.

- [ ] **Step 3: Implement**

In `internal/session/sessionservice.go`:

1. Delete `sessionBranchIDs` and replace its three call sites — in `DismissSession`, `SessionSnapshot`, `SyncPRActivity` — with `sess.BranchIDs()`.
2. Delete `sessionBranchName` and add:

```go
func (s *SessionService) prBranchName(pr *git.PullRequest) string {
	if pr.HeadBranch != nil {
		return pr.HeadBranch.Name
	}
	if pr.HeadBranchID == nil {
		return ""
	}
	branch, err := s.gitService.GetBranch(*pr.HeadBranchID)
	if err != nil {
		return ""
	}
	return branch.Name
}
```

3. `notifyPRUpdated` becomes:

```go
func (s *SessionService) notifyPRUpdated(ctx context.Context, sess *Session, pr *git.PullRequest, previous *git.PullRequest) {
	data := git.NewPRNotification(pr, previous, s.prBranchName(pr))
	s.publishNotification(ctx, sess.ID, git.NotificationPullRequest, data)
}
```

4. `SessionSnapshot`'s loop becomes:

```go
	var out []eventbus.SessionNotificationEvent
	for _, branchID := range sess.BranchIDs() {
		for _, pr := range s.gitService.GetPRsForBranch(branchID) {
			if !pr.State.IsOpen() {
				continue
			}
			out = append(out, eventbus.SessionNotificationEvent{
				SessionID: sessionID,
				Type:      git.NotificationPullRequest,
				Data:      git.NewPRNotification(&pr, nil, s.prBranchName(&pr)),
			})
		}
	}
	return out
```

5. In `handlePRUpdated`, delete the `newlyMerged` variable and the `if newlyMerged { s.maybeCompleteSession(sess, pr) }` block. Delete `maybeCompleteSession`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS. The existing `TestHandlePRUpdated_PublishesSessionNotification` and `TestSessionSnapshot_ReturnsSessionPRs` still expect `Branch: "feature-pr"` and must pass via the `GetBranch` fallback.

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat(session): attribute PRs across all session branches; stop auto-completing"
```

---

### Task 4: Branch detection — `RecordWorktreeBranch`, reconciliation, triggers

**Files:**
- Create: `internal/session/sessionbranchsync.go`
- Modify: `internal/session/sessionservice.go` — `OnAppStart` (~102-119), `ActivateSession` (~1056)
- Modify: `internal/session/sessionmodule.go` — new job + `RegisterJobs`
- Test: `internal/session/sessionbranchsync_test.go` (create)

**Interfaces:**
- Consumes: `GitService.DefaultBranch`, `GitService.RepointWorktree`, `GitService.CurrentBranch`, `GitService.FindOrCreateBranch(ctx, name string, repoID uint) (*git.Branch, error)`, `GitService.GetPRsForBranch`, `SessionBranchStore.Add`, `git.NewPRNotification`
- Produces:
  - `func (s *SessionService) RecordWorktreeBranch(ctx context.Context, path string, branchName string) error`
  - `func (s *SessionService) SyncSessionBranches(ctx context.Context, sess *Session)`
  - `func (s *SessionService) SyncAllSessionBranches(ctx context.Context) error`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/sessionbranchsync_test.go`:

```go
package session

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/git"
	"github.com/stretchr/testify/require"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(out))
}

type branchSyncEnv struct {
	service  *SessionService
	store    *SessionStore
	sess     *Session
	repoPath string
	wtPath   string
}

func setupBranchSyncEnv(t *testing.T) *branchSyncEnv {
	t.Helper()
	repoPath := initTestRepo(t)
	gitIn(t, repoPath, "remote", "set-head", "origin", "main")
	service, store, _, wsID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess := &Session{Name: "multi"}
	require.NoError(t, createSessionLegacy(context.Background(), service, sess, wsID, "", "main"))
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	loaded, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	return &branchSyncEnv{
		service:  service,
		store:    store,
		sess:     loaded,
		repoPath: repoPath,
		wtPath:   loaded.Worktrees[0].Worktree.Path,
	}
}

func (env *branchSyncEnv) reload(t *testing.T) *Session {
	t.Helper()
	sess, err := env.store.GetByID(env.sess.ID)
	require.NoError(t, err)
	return sess
}

func branchNames(sess *Session) []string {
	var names []string
	for _, sb := range sess.Branches {
		if sb.Branch != nil {
			names = append(names, sb.Branch.Name)
		}
	}
	return names
}

func TestRecordWorktreeBranch_RecordsOldAndNewAndRepoints(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/second")

	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/second"))

	sess := env.reload(t)
	require.ElementsMatch(t, []string{"eqt/multi", "eqt/second"}, branchNames(sess))
	require.Equal(t, "eqt/second", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_MatchesCanonicalPath(t *testing.T) {
	env := setupBranchSyncEnv(t)
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/canonical")
	canonical, err := filepath.EvalSymlinks(env.wtPath)
	require.NoError(t, err)

	require.NoError(t, env.service.RecordWorktreeBranch(context.Background(), canonical, "eqt/canonical"))

	require.Equal(t, "eqt/canonical", env.reload(t).Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_SkipsDetachedAndDefault(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()

	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, ""))
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "main"))

	sess := env.reload(t)
	require.Empty(t, sess.Branches)
	require.Equal(t, "eqt/multi", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_IgnoresUnknownPath(t *testing.T) {
	env := setupBranchSyncEnv(t)
	require.NoError(t, env.service.RecordWorktreeBranch(context.Background(), t.TempDir(), "eqt/elsewhere"))
	require.Empty(t, env.reload(t).Branches)
}

func TestRecordWorktreeBranch_SwitchBackIsIdempotent(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/b")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/b"))
	gitIn(t, env.wtPath, "checkout", "eqt/multi")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/multi"))

	sess := env.reload(t)
	require.ElementsMatch(t, []string{"eqt/multi", "eqt/b"}, branchNames(sess))
	require.Equal(t, "eqt/multi", sess.Worktrees[0].Worktree.Branch.Name)
}

func TestRecordWorktreeBranch_ReplaysOpenPRsForNewBranch(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	var got []string
	env.service.eventBus.Subscribe(eventbus.SessionNotification, func(_ context.Context, event eventbus.Event) error {
		if data, ok := event.Data.(eventbus.SessionNotificationEvent); ok {
			if n, ok := data.Data.(git.PRNotification); ok {
				got = append(got, n.Branch)
			}
		}
		return nil
	})

	branch, err := env.service.gitService.FindOrCreateBranch(ctx, "eqt/with-pr", env.sess.Worktrees[0].Worktree.RepoID)
	require.NoError(t, err)
	branchID := branch.ID
	require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: branch.RepoID, Number: 3, HeadBranchID: &branchID, State: git.PRStateOpen}).Error)
	require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: branch.RepoID, Number: 4, HeadBranchID: &branchID, State: git.PRStateMerged}).Error)

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/with-pr")
	require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, "eqt/with-pr"))

	require.Equal(t, []string{"eqt/with-pr"}, got)
}

func TestSyncSessionBranches_PicksUpBranchChangedOnDisk(t *testing.T) {
	env := setupBranchSyncEnv(t)
	gitIn(t, env.wtPath, "checkout", "-b", "eqt/from-disk")

	env.service.SyncSessionBranches(context.Background(), env.sess)

	require.Equal(t, "eqt/from-disk", env.reload(t).Worktrees[0].Worktree.Branch.Name)
}
```

Add `"github.com/eleonorayaya/utena/internal/eventbus"` to that file's imports.

Notes for the implementer:
- `createSessionLegacy(..., "", "main")` creates branch `eqt/multi` (prefix `eqt/` + session name) from `main` in `<sessionsRoot>/multi/git-repo`, and pushes it.
- `remote set-head origin main` makes `DefaultBranch` return `main` for this non-bare repo.
- On macOS `t.TempDir()` lives under `/var/...`, a symlink to `/private/var/...`, so the stored worktree path is non-canonical and the canonical-path test is meaningful.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `env.service.RecordWorktreeBranch undefined`, `env.service.SyncSessionBranches undefined`.

- [ ] **Step 3: Implement**

Create `internal/session/sessionbranchsync.go`:

```go
package session

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/eleonorayaya/utena/internal/git"
)

func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

func (s *SessionService) RecordWorktreeBranch(ctx context.Context, path string, branchName string) error {
	if branchName == "" || path == "" {
		return nil
	}
	sess, swt, err := s.findSessionWorktreeByPath(path)
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}
	return s.recordBranch(ctx, sess, swt, branchName)
}

func (s *SessionService) findSessionWorktreeByPath(path string) (*Session, *SessionWorktree, error) {
	target := canonicalPath(path)
	sessions, err := s.store.List()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted {
			continue
		}
		for j := range sess.Worktrees {
			wt := sess.Worktrees[j].Worktree
			if wt != nil && wt.Path != "" && canonicalPath(wt.Path) == target {
				return sess, &sess.Worktrees[j], nil
			}
		}
	}
	return nil, nil, nil
}

func (s *SessionService) recordBranch(ctx context.Context, sess *Session, swt *SessionWorktree, branchName string) error {
	wt := swt.Worktree
	if wt == nil || branchName == "" {
		return nil
	}
	if wt.Branch != nil && wt.Branch.Name == branchName {
		return nil
	}
	ws := swt.Workspace
	if ws == nil {
		return fmt.Errorf("worktree %d has no workspace", wt.ID)
	}
	if branchName == s.gitService.DefaultBranch(ctx, ws.Path) {
		return nil
	}

	branch, err := s.gitService.FindOrCreateBranch(ctx, branchName, wt.RepoID)
	if err != nil {
		return fmt.Errorf("find-or-create branch %q: %w", branchName, err)
	}

	now := time.Now()
	for _, id := range []uint{wt.BranchID, branch.ID} {
		if id == 0 {
			continue
		}
		if err := s.sessionBranchStore.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: wt.ID, BranchID: id, FirstSeenAt: now}); err != nil {
			return fmt.Errorf("record session branch: %w", err)
		}
	}

	if err := s.gitService.RepointWorktree(wt, branch); err != nil {
		slog.WarnContext(ctx, "failed to repoint worktree to new branch", "session", sess.ID, "worktree", wt.ID, "branch", branchName, "error", err)
	}
	slog.InfoContext(ctx, "session branch attached", "session", sess.ID, "worktree", wt.ID, "branch", branchName)

	s.replayOpenPRs(ctx, sess.ID, branch)
	return nil
}

func (s *SessionService) replayOpenPRs(ctx context.Context, sessionID uint, branch *git.Branch) {
	for _, pr := range s.gitService.GetPRsForBranch(branch.ID) {
		if !pr.State.IsOpen() {
			continue
		}
		s.publishNotification(ctx, sessionID, git.NotificationPullRequest, git.NewPRNotification(&pr, nil, branch.Name))
	}
}

func (s *SessionService) SyncSessionBranches(ctx context.Context, sess *Session) {
	for i := range sess.Worktrees {
		swt := &sess.Worktrees[i]
		wt := swt.Worktree
		if wt == nil || wt.Path == "" || wt.Status != git.WorktreeStatusPresent {
			continue
		}
		name, err := s.gitService.CurrentBranch(ctx, wt.Path)
		if err != nil {
			slog.DebugContext(ctx, "branch sync: current branch lookup failed", "session", sess.ID, "worktree", wt.ID, "error", err)
			continue
		}
		if err := s.recordBranch(ctx, sess, swt, name); err != nil {
			slog.WarnContext(ctx, "branch sync failed", "session", sess.ID, "worktree", wt.ID, "error", err)
		}
	}
}

func (s *SessionService) SyncAllSessionBranches(ctx context.Context) error {
	sessions, err := s.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	for i := range sessions {
		if sessions[i].watchesPRActivity() {
			s.SyncSessionBranches(ctx, &sessions[i])
		}
	}
	return nil
}
```

In `internal/session/sessionservice.go` `OnAppStart`, after `s.reconcileTmuxState(ctx, sessions)`:

```go
	for i := range sessions {
		if sessions[i].watchesPRActivity() {
			s.SyncSessionBranches(ctx, &sessions[i])
		}
	}
```

In `ActivateSession`, right after `s.ensureSessionWorktrees(ctx, session)`:

```go
	s.SyncSessionBranches(ctx, session)
```

In `internal/session/sessionmodule.go`, add:

```go
type sessionBranchesTask struct {
	service *SessionService
}

func (t *sessionBranchesTask) Name() string            { return "session.branches" }
func (t *sessionBranchesTask) Interval() time.Duration { return 2 * time.Minute }
func (t *sessionBranchesTask) Run(ctx context.Context) error {
	return t.service.SyncAllSessionBranches(ctx)
}
```

and register it in `RegisterJobs`:

```go
	svc.Register(&sessionBranchesTask{service: m.Service})
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat(session): detect worktree branch changes and record them"
```

---

### Task 5: `PUT /sessions/branch-hook` endpoint

**Files:**
- Modify: `internal/session/types.go` (request type)
- Modify: `internal/session/sessioncontroller.go` (handler)
- Modify: `internal/session/sessionrouter.go:20-30` (route)
- Test: `internal/session/sessionrouter_test.go`

**Interfaces:**
- Consumes: `SessionService.RecordWorktreeBranch` (Task 4)
- Produces: `PUT /sessions/branch-hook` accepting `{"path": string, "branch": string}`; 204 on success, 400 when `path` is empty

- [ ] **Step 1: Write the failing tests**

Append to `internal/session/sessionrouter_test.go`:

```go
func TestSessionRouter_BranchHook_UnknownPathReturnsNoContent(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := bytes.NewBufferString(`{"path":"/not/a/session","branch":"eqt/x"}`)
	req := httptest.NewRequest(http.MethodPut, "/branch-hook", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestSessionRouter_BranchHook_RequiresPath(t *testing.T) {
	router, _, _, _, _, _ := setupSessionRouter(t)

	body := bytes.NewBufferString(`{"branch":"eqt/x"}`)
	req := httptest.NewRequest(http.MethodPut, "/branch-hook", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.Routes().ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}
```

(`setupSessionRouter` builds its DB with `setupTestDB`, which Task 2 already updated.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — 405/404 status instead of 204/400.

- [ ] **Step 3: Implement**

Add to `internal/session/types.go`:

```go
type BranchHookRequest struct {
	Path   string `json:"path"`
	Branch string `json:"branch"`
}

func (r *BranchHookRequest) Bind(_ *http.Request) error {
	if r.Path == "" {
		return errors.New("path is required")
	}
	return nil
}
```

Add to `internal/session/sessioncontroller.go`:

```go
func (c *SessionController) BranchHook(w http.ResponseWriter, r *http.Request) {
	data := &BranchHookRequest{}
	if err := render.Bind(r, data); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	if err := c.service.RecordWorktreeBranch(r.Context(), data.Path, data.Branch); err != nil {
		common.RenderError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
```

In `internal/session/sessionrouter.go`, add before the `/{id}` routes:

```go
	r.Put("/branch-hook", sr.controller.BranchHook)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat(session): add branch-hook endpoint"
```

---

### Task 6: `utena hook post-checkout` CLI

**Files:**
- Create: `cmd/tui/hook.go`
- Modify: `cmd/tui/main.go:34-42` (register command)
- Test: `cmd/tui/hook_test.go` (create)

**Interfaces:**
- Consumes: `PUT /sessions/branch-hook` (Task 5)
- Produces: `utena hook post-checkout <prev> <new> <flag>`; `func reportCheckout(ctx context.Context, baseURL string, args []string)`

- [ ] **Step 1: Write the failing tests**

Create `cmd/tui/hook_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func initHookTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=T", "commit", "--allow-empty", "-m", "init"},
		{"checkout", "-b", "eqt/hooked"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, string(out))
	}
	return dir
}

func TestReportCheckout_SendsPathAndBranch(t *testing.T) {
	dir := initHookTestRepo(t)
	t.Chdir(dir)

	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/sessions/branch-hook", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	reportCheckout(context.Background(), server.URL, []string{"a", "b", "1"})

	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	require.Equal(t, canonical, got["path"])
	require.Equal(t, "eqt/hooked", got["branch"])
}

func TestReportCheckout_IgnoresFileCheckout(t *testing.T) {
	t.Chdir(initHookTestRepo(t))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	reportCheckout(context.Background(), server.URL, []string{"a", "b", "0"})
	reportCheckout(context.Background(), server.URL, nil)

	require.False(t, called)
}

func TestReportCheckout_UnreachableDaemon(t *testing.T) {
	t.Chdir(initHookTestRepo(t))
	start := time.Now()

	reportCheckout(context.Background(), "http://127.0.0.1:1", []string{"a", "b", "1"})

	require.Less(t, time.Since(start), 2*time.Second)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test`
Expected: FAIL — `undefined: reportCheckout`.

- [ ] **Step 3: Implement**

Create `cmd/tui/hook.go`:

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const branchCheckoutFlag = "1"

func hookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:    "hook",
		Short:  "Git hook entry points",
		Hidden: true,
	}
	cmd.AddCommand(postCheckoutHookCmd())
	return cmd
}

func postCheckoutHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "post-checkout [prev] [new] [flag]",
		Short:         "Report a branch checkout to the utena daemon",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			port, _ := cmd.Root().Flags().GetString("port")
			reportCheckout(cmd.Context(), "http://localhost:"+port, args)
			return nil
		},
	}
}

func reportCheckout(ctx context.Context, baseURL string, args []string) {
	if len(args) < 3 || args[2] != branchCheckoutFlag {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()

	toplevel, err := gitOutput(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return
	}
	branch, err := gitOutput(ctx, "branch", "--show-current")
	if err != nil {
		return
	}

	body, err := json.Marshal(map[string]string{"path": toplevel, "branch": branch})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, baseURL+"/sessions/branch-hook", bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if res, err := http.DefaultClient.Do(req); err == nil {
		res.Body.Close()
	}
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
```

In `cmd/tui/main.go`, register it with the other commands:

```go
	rootCmd.AddCommand(hookCmd())
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/tui/hook.go cmd/tui/hook_test.go cmd/tui/main.go
git commit -m "feat(cli): add utena hook post-checkout"
```

---

### Task 7: Delete-with-branch cleans up past branches safely

**Files:**
- Modify: `internal/session/sessionservice.go` — `cleanupSessionResources` (~1173-1205); add `cleanupPastBranches`, `pastBranchDeletable`
- Test: `internal/session/sessionbranchsync_test.go`

**Interfaces:**
- Consumes: `Session.PastBranches()` (Task 2), `GitService.HasUnpushedCommits`, `GitService.HasBranch`, `GitService.CleanupBranch`, `GitService.GetPRsForBranch`, `recordBranch` via `RecordWorktreeBranch` (Task 4)

- [ ] **Step 1: Write the failing test**

Append to `internal/session/sessionbranchsync_test.go`:

```go
func localBranchExists(t *testing.T, repo string, name string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/"+name)
	return cmd.Run() == nil
}

func TestDeleteSession_DeletesPastBranchesOnlyWhenSafe(t *testing.T) {
	env := setupBranchSyncEnv(t)
	ctx := context.Background()
	record := func(name string) {
		require.NoError(t, env.service.RecordWorktreeBranch(ctx, env.wtPath, name))
	}

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/pushed-past")
	record("eqt/pushed-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/merged-past")
	gitIn(t, env.wtPath, "commit", "--allow-empty", "-m", "merged work")
	record("eqt/merged-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/unpushed-past", "main")
	gitIn(t, env.wtPath, "commit", "--allow-empty", "-m", "local only")
	record("eqt/unpushed-past")

	gitIn(t, env.wtPath, "checkout", "-b", "eqt/current", "main")
	record("eqt/current")

	sess := env.reload(t)
	for _, b := range sess.PastBranches() {
		if b.Name == "eqt/merged-past" {
			id := b.ID
			require.NoError(t, env.store.db.Create(&git.PullRequest{RepoID: b.RepoID, Number: 11, HeadBranchID: &id, State: git.PRStateMerged}).Error)
		}
	}

	_, err := env.service.ArchiveSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NoError(t, env.service.DeleteSession(ctx, sess.ID, true, false))

	require.False(t, localBranchExists(t, env.repoPath, "eqt/current"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/multi"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/pushed-past"))
	require.False(t, localBranchExists(t, env.repoPath, "eqt/merged-past"))
	require.True(t, localBranchExists(t, env.repoPath, "eqt/unpushed-past"), "unmerged local work must survive")
}
```

Why each branch ends up where it does: `eqt/multi` was pushed at session creation; `eqt/pushed-past` has no commits beyond `main`; `eqt/merged-past` has a local-only commit but a merged PR; `eqt/unpushed-past` has a local-only commit and no PR; `eqt/current` is the worktree's branch and is force-deleted as today.

- [ ] **Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — `eqt/multi` / `eqt/pushed-past` / `eqt/merged-past` still exist.

- [ ] **Step 3: Implement**

In `internal/session/sessionservice.go`, inside `cleanupSessionResources`, after the worktree `for` loop and before `s.removeSessionRoot(session)`:

```go
	if deleteBranch {
		s.cleanupPastBranches(ctx, session, wsCache)
	}
```

Add:

```go
func (s *SessionService) cleanupPastBranches(ctx context.Context, session *Session, wsCache map[uint]*workspace.Workspace) {
	for _, branch := range session.PastBranches() {
		ws, ok := wsCache[branch.RepoID]
		if !ok {
			found, err := s.workspaceService.GetWorkspaceByRepoID(ctx, branch.RepoID)
			if err != nil {
				slog.Warn("cleanup: failed to resolve workspace for past branch", "branch", branch.Name, "error", err)
				continue
			}
			ws = found
			wsCache[branch.RepoID] = ws
		}
		exists, err := s.gitService.HasBranch(ctx, ws.Path, branch.Name)
		if err != nil || !exists {
			continue
		}
		if !s.pastBranchDeletable(ctx, &branch, ws.Path) {
			slog.Warn("keeping past session branch with unpushed work", "session", session.ID, "branch", branch.Name)
			continue
		}
		if err := s.gitService.CleanupBranch(ctx, &branch, ws.Path, true); err != nil {
			slog.Warn("failed to delete past session branch", "session", session.ID, "branch", branch.Name, "error", err)
		}
	}
}

func (s *SessionService) pastBranchDeletable(ctx context.Context, branch *git.Branch, repoPath string) bool {
	for _, pr := range s.gitService.GetPRsForBranch(branch.ID) {
		if pr.State == git.PRStateMerged {
			return true
		}
	}
	unpushed, err := s.gitService.HasUnpushedCommits(ctx, repoPath, branch.Name)
	return err == nil && !unpushed
}
```

`wsCache` is keyed by repo ID, the same keying `resolveWorktreeWorkspace` uses, so the two share one cache safely.

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS (including the existing `TestSessionService_DeleteSession_*` tests).

- [ ] **Step 5: Commit**

```bash
git add internal/session/
git commit -m "feat(session): delete past session branches only when no work is lost"
```

---

### Task 8: TUI — show PRs from all session branches and list past branches

**Files:**
- Modify: `internal/tui/sessiondetail/model.go` — `PRsStateUpdatedMsg` case (~117-121), delete `filterPRsByBranch` (~195-206), `View` (~252-268)
- Modify: `internal/tui/sessionlist/sessionlist.go` — `PRsStateUpdatedMsg` case (~172-182), delete `filterPRsByBranch` (~349-360)
- Test: `internal/tui/sessiondetail/model_test.go`

**Interfaces:**
- Consumes: `Session.FilterPRs`, `Session.PastBranches` (Task 2)

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/sessiondetail/model_test.go` (add `"github.com/eleonorayaya/utena/internal/git"` to its imports):

```go
func TestRenderPanel_ShowsPastBranchesAndTheirPRs(t *testing.T) {
	current, past := uint(2), uint(1)
	s := session.Session{
		Model:  gorm.Model{ID: 9},
		Name:   "stacked",
		Status: session.StatusActive,
		Worktrees: []session.SessionWorktree{{
			Worktree: &git.Worktree{BranchID: current, Branch: &git.Branch{Model: gorm.Model{ID: current}, Name: "eqt/top"}},
		}},
		Branches: []session.SessionBranch{
			{BranchID: past, Branch: &git.Branch{Model: gorm.Model{ID: past}, Name: "eqt/base"}},
			{BranchID: current, Branch: &git.Branch{Model: gorm.Model{ID: current}, Name: "eqt/top"}},
		},
	}
	prs := s.FilterPRs([]git.PullRequest{
		{Number: 1, Title: "base", HeadBranchID: &past, State: git.PRStateOpen},
		{Number: 2, Title: "top", HeadBranchID: &current, State: git.PRStateDraft},
	})

	out := RenderPanel(&s, prs, 80, 0)

	assert.Contains(t, out, "eqt/top")
	assert.Contains(t, out, "Past")
	assert.Contains(t, out, "eqt/base")
	assert.Contains(t, out, "#1 base")
	assert.Contains(t, out, "#2 top")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test`
Expected: FAIL — output lacks "Past" / "eqt/base".

- [ ] **Step 3: Implement**

In `internal/tui/sessiondetail/model.go`, the `provider.PRsStateUpdatedMsg` case becomes:

```go
	case provider.PRsStateUpdatedMsg:
		if m.sess != nil && len(m.sess.Worktrees) > 0 && m.sess.Worktrees[0].Workspace != nil && msg.WorkspaceID == m.sess.Worktrees[0].Workspace.ID {
			m.prs = m.sess.FilterPRs(msg.PullRequests)
		}
		return m, nil
```

Delete `filterPRsByBranch` from that file. In `View`, directly after the `for i := range s.Worktrees { ... }` loop (inside the `if len(s.Worktrees) > 0` branch), add:

```go
		mutedStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		for _, past := range s.PastBranches() {
			b.WriteString(labelStyle().Render("Past") + mutedStyle.Render(past.Name) + "\n")
		}
```

In `internal/tui/sessionlist/sessionlist.go`, the `provider.PRsStateUpdatedMsg` case becomes:

```go
	case provider.PRsStateUpdatedMsg:
		sel := m.selectedSession()
		if sel != nil && len(sel.Worktrees) > 0 && sel.Worktrees[0].Workspace != nil &&
			msg.WorkspaceID == sel.Worktrees[0].Workspace.ID {
			m.prs = sel.FilterPRs(msg.PullRequests)
		}
		return m, nil
```

Delete `filterPRsByBranch` from that file, and remove the `git` import from either file if it becomes unused (the compiler will say).

- [ ] **Step 4: Run tests to verify they pass**

Run: `task test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/
git commit -m "feat(tui): show PRs and past branches for multi-branch sessions"
```

---

### Task 9: Dotfiles — install the global git hook (dotfiles repo)

**Repo:** dotfiles (shizuku)

**Files:**
- Modify: `programs/utena/utena.go` (`Sync`, add `configureGitHook`)
- Test: `programs/utena/utena_test.go` (create)

**Interfaces:**
- Consumes: `utena hook post-checkout` (Task 6) — the hook calls the installed `utena` binary.
- Produces: global git config `[hook "utena"] command = utena hook post-checkout`, `event = post-checkout`.

The hook lives in the `utena` program, not `programs/git`, because `git` is in the server-safe base profile while `utena` is only in `desktop`. The git CLI is used instead of `desiredGitConfigs`, whose ini merge would write `[hook.utena]` rather than `[hook "utena"]`.

- [ ] **Step 1: Write the failing test**

Create `programs/utena/utena_test.go`:

```go
package utena

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitConfigGetAll(t *testing.T, key string) []string {
	t.Helper()
	out, err := exec.Command("git", "config", "--global", "--get-all", key).Output()
	if err != nil {
		t.Fatalf("git config --get-all %s: %v", key, err)
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func TestConfigureGitHook_IsIdempotent(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))

	for range 2 {
		if err := configureGitHook(); err != nil {
			t.Fatalf("configureGitHook: %v", err)
		}
	}

	if got := gitConfigGetAll(t, "hook.utena.command"); len(got) != 1 || got[0] != "utena hook post-checkout" {
		t.Fatalf("hook.utena.command = %v", got)
	}
	if got := gitConfigGetAll(t, "hook.utena.event"); len(got) != 1 || got[0] != "post-checkout" {
		t.Fatalf("hook.utena.event = %v", got)
	}
}
```

(Check whether other dotfiles tests use testify — `grep -rn testify --include='*_test.go' .` from the dotfiles root. If they do, switch to `require` to match.)

- [ ] **Step 2: Run test to verify it fails**

Run (from the dotfiles root): `task test`
Expected: FAIL — `undefined: configureGitHook`.

- [ ] **Step 3: Implement**

In `programs/utena/utena.go`, add:

```go
var gitHookConfig = [][]string{
	{"config", "--global", "hook.utena.command", "utena hook post-checkout"},
	{"config", "--global", "--replace-all", "hook.utena.event", "post-checkout"},
}

func configureGitHook() error {
	for _, args := range gitHookConfig {
		cmd := exec.Command("git", args...)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("failed to set git hook config %v: %s: %w", args, string(output), err)
		}
	}
	return nil
}
```

and at the end of `Sync`, before `return nil`:

```go
	if err := configureGitHook(); err != nil {
		return fmt.Errorf("failed to configure utena git hook: %w", err)
	}
```

- [ ] **Step 4: Run tests and lint**

Run (from the dotfiles root): `task test` then `task lint`
Expected: PASS, no lint changes.

- [ ] **Step 5: Commit (dotfiles repo)**

```bash
git add programs/utena/utena.go programs/utena/utena_test.go
git commit -m "feat(utena): install global post-checkout hook for session branch tracking"
```

- [ ] **Step 6: Note the manual check for the user (do not run sync yourself)**

Running `shizuku sync` writes to the real `~/.gitconfig`, so leave it to the user. In the final summary, ask them to:
1. Run `/task run sync`, then `git config --global --get-all hook.utena.event` (expect exactly one `post-checkout`).
2. Run `/task run diff` and confirm the `git` program shows no change to `~/.gitconfig`. This checks that its ini.v1 re-merge round-trips the `[hook "utena"]` section.

---

## Final verification

- [ ] From the utena root: `task fmt` then `task test` — all green.
- [ ] From the dotfiles root: `task lint` then `task test` — all green.
- [ ] `grep -rn "maybeCompleteSession\|sessionBranchName\|sessionBranchIDs\|filterPRsByBranch" internal` (utena root) — no output.
- [ ] `grep -n 'exec.CommandContext(ctx, "git"' internal/git/gitcli.go` — no output.
