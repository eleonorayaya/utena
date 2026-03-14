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
	run(t, dir, "git", "init")
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

func TestGitService_ListBranches(t *testing.T) {
	repo := initTestRepo(t)
	run(t, repo, "git", "branch", "develop")
	run(t, repo, "git", "branch", "feature-x")

	svc := NewGitService()
	branches, err := svc.ListBranches(context.Background(), repo)
	require.NoError(t, err)
	require.Contains(t, branches, "main")
	require.Contains(t, branches, "develop")
	require.Contains(t, branches, "feature-x")
}

func TestGitService_ListBranches_MainFirst(t *testing.T) {
	repo := initTestRepo(t)
	run(t, repo, "git", "branch", "alpha")
	run(t, repo, "git", "branch", "zebra")

	svc := NewGitService()
	branches, err := svc.ListBranches(context.Background(), repo)
	require.NoError(t, err)
	require.Equal(t, "main", branches[0])
}

func TestGitService_ListBranches_NoMainBranch(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init", "-b", "trunk")
	run(t, dir, "git", "config", "user.email", "test@test.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")

	svc := NewGitService()
	branches, err := svc.ListBranches(context.Background(), dir)
	require.NoError(t, err)
	require.Equal(t, []string{"trunk"}, branches)
}

func TestGitService_ListBranches_EmptyRepo(t *testing.T) {
	dir := t.TempDir()
	run(t, dir, "git", "init")

	svc := NewGitService()
	branches, err := svc.ListBranches(context.Background(), dir)
	require.NoError(t, err)
	require.Empty(t, branches)
}

func TestGitService_ListBranches_InvalidPath(t *testing.T) {
	svc := NewGitService()
	_, err := svc.ListBranches(context.Background(), "/nonexistent/path")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to list branches")
}

func TestGitService_CreateWorktree(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	worktreePath, err := svc.CreateWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(repo, ".worktrees", "eqt-my-feature"), worktreePath)

	info, err := os.Stat(worktreePath)
	require.NoError(t, err)
	require.True(t, info.IsDir())

	branchOut, err := exec.Command("git", "-C", worktreePath, "branch", "--show-current").Output()
	require.NoError(t, err)
	require.Equal(t, "eqt/my-feature", trimOutput(branchOut))
}

func TestGitService_CreateWorktree_DuplicateName(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	_, err := svc.CreateWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.NoError(t, err)

	_, err = svc.CreateWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitService_CreateWorktree_InvalidBaseBranch(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	_, err := svc.CreateWorktree(context.Background(), repo, "eqt/my-feature", "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitService_Pull_NoRemote(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	err := svc.Pull(context.Background(), repo, "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git pull failed")
}

func TestGitService_FindWorktreeByBranch(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	_, err := svc.CreateWorktree(context.Background(), repo, "eqt/my-feature", "main")
	require.NoError(t, err)

	path, err := svc.FindWorktreeByBranch(context.Background(), repo, "eqt/my-feature")
	require.NoError(t, err)

	resolvedExpected, _ := filepath.EvalSymlinks(filepath.Join(repo, ".worktrees", "eqt-my-feature"))
	require.Equal(t, resolvedExpected, path)
}

func TestGitService_FindWorktreeByBranch_NotFound(t *testing.T) {
	repo := initTestRepo(t)

	svc := NewGitService()
	path, err := svc.FindWorktreeByBranch(context.Background(), repo, "nonexistent")
	require.NoError(t, err)
	require.Empty(t, path)
}

func TestGitService_FindWorktreeByBranch_MainWorktree(t *testing.T) {
	repo := initTestRepo(t)
	resolvedRepo, err := filepath.EvalSymlinks(repo)
	require.NoError(t, err)

	svc := NewGitService()
	path, err := svc.FindWorktreeByBranch(context.Background(), repo, "main")
	require.NoError(t, err)
	require.Equal(t, resolvedRepo, path)
}

func trimOutput(b []byte) string {
	s := string(b)
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
