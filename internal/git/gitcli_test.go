package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func initTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "command %q failed: %s", name+" "+joinArgs(args), string(out))
}

func joinArgs(args []string) string {
	s := ""
	for i, a := range args {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}

func trimOutput(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func TestGitCLI_ListBranches(t *testing.T) {
	repo := initTestRepo(t)
	run(t, repo, "git", "branch", "develop")
	run(t, repo, "git", "branch", "feature-x")

	svc := newGitCLI()
	branches, err := svc.listBranches(context.Background(), repo)
	require.NoError(t, err)
	require.Contains(t, branches, "main")
	require.Contains(t, branches, "develop")
	require.Contains(t, branches, "feature-x")
}

func TestGitCLI_ListBranches_MainFirst(t *testing.T) {
	repo := initTestRepo(t)
	run(t, repo, "git", "branch", "alpha")
	run(t, repo, "git", "branch", "zebra")

	svc := newGitCLI()
	branches, err := svc.listBranches(context.Background(), repo)
	require.NoError(t, err)
	require.Equal(t, "main", branches[0])
}

func TestGitCLI_ListBranches_NoMainBranch(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "trunk")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")

	svc := newGitCLI()
	branches, err := svc.listBranches(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{"trunk"}, branches)
}

func TestGitCLI_ListBranches_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")

	svc := newGitCLI()
	branches, err := svc.listBranches(context.Background(), dir)
	require.NoError(t, err)
	require.Empty(t, branches)
}

func TestGitCLI_ListBranches_InvalidPath(t *testing.T) {
	svc := newGitCLI()
	_, err := svc.listBranches(context.Background(), "/nonexistent/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to list branches")
}

func TestGitCLI_CreateWorktree(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	worktreePath, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(repo, ".worktrees", "eqt-my-feature"), worktreePath)

	info, err := os.Stat(worktreePath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	branchOut, err := exec.Command("git", "-C", worktreePath, "branch", "--show-current").Output()
	require.NoError(t, err)
	require.Equal(t, "eqt/my-feature", trimOutput(branchOut))
}

func TestGitCLI_CreateWorktree_DuplicateName(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	_, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.NoError(t, err)

	_, err = svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitCLI_CreateWorktree_InvalidBaseBranch(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	_, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitCLI_Pull_NoRemote(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	err := svc.pull(context.Background(), repo, "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git pull failed")
}

func TestGitCLI_WorktreePath(t *testing.T) {
	svc := newGitCLI()
	require.Equal(t, "/repo/.worktrees/eqt-my-feature", svc.worktreePath("/repo", "eqt/my-feature"))
	require.Equal(t, "/repo/.worktrees/simple-branch", svc.worktreePath("/repo", "simple-branch"))
}

func TestGitCLI_ParseRepoFullName_HTTPS(t *testing.T) {
	svc := newGitCLI()
	owner, repo, err := svc.parseRepoFullName("https://github.com/eleonorayaya/utena.git")
	require.NoError(t, err)
	require.Equal(t, "eleonorayaya", owner)
	require.Equal(t, "utena", repo)
}

func TestGitCLI_ParseRepoFullName_SSH(t *testing.T) {
	svc := newGitCLI()
	owner, repo, err := svc.parseRepoFullName("git@github.com:eleonorayaya/utena.git")
	require.NoError(t, err)
	require.Equal(t, "eleonorayaya", owner)
	require.Equal(t, "utena", repo)
}

func TestGitCLI_ParseRepoFullName_NoGitSuffix(t *testing.T) {
	svc := newGitCLI()

	owner, repo, err := svc.parseRepoFullName("https://github.com/eleonorayaya/utena")
	require.NoError(t, err)
	require.Equal(t, "eleonorayaya", owner)
	require.Equal(t, "utena", repo)

	owner, repo, err = svc.parseRepoFullName("git@github.com:eleonorayaya/utena")
	require.NoError(t, err)
	require.Equal(t, "eleonorayaya", owner)
	require.Equal(t, "utena", repo)
}

func TestGitCLI_ParseRepoFullName_InvalidURL(t *testing.T) {
	svc := newGitCLI()

	_, _, err := svc.parseRepoFullName("not-a-url")
	require.Error(t, err)

	_, _, err = svc.parseRepoFullName("")
	require.Error(t, err)

	_, _, err = svc.parseRepoFullName("https://github.com/only-owner")
	require.Error(t, err)

	_, _, err = svc.parseRepoFullName("git@github.com:only-owner")
	require.Error(t, err)
}
