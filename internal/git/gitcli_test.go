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
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	run(t, dir, "git", "config", "tag.gpgsign", "false")
	run(t, dir, "git", "commit", "--allow-empty", "-m", "init")
	return dir
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
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
	worktreePath, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main", "")
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
	_, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main", "")
	require.NoError(t, err)

	_, err = svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitCLI_CreateWorktree_InvalidBaseBranch(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	_, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "nonexistent", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git worktree add failed")
}

func TestGitCLI_ValidateWorktree_DetachedHead(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	worktreePath, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main", "")
	require.NoError(t, err)

	run(t, worktreePath, "git", "checkout", "--detach")

	exists, err := svc.validateWorktree(worktreePath)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestGitCLI_ValidateWorktree_DifferentBranch(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	worktreePath, err := svc.createWorktree(context.Background(), repo, "eqt/my-feature", "main", "")
	require.NoError(t, err)

	run(t, worktreePath, "git", "checkout", "-b", "eqt/something-else")

	exists, err := svc.validateWorktree(worktreePath)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestGitCLI_Pull_NoRemote(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	err := svc.pull(context.Background(), repo, "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "git pull failed")
}

func TestGitCLI_FetchOrigin_NoRemote(t *testing.T) {
	repo := initTestRepo(t)

	svc := newGitCLI()
	err := svc.fetchOrigin(context.Background(), repo)
	require.Error(t, err)
	require.Contains(t, err.Error(), "git fetch failed")
}

func initTestRepoWithOrigin(t *testing.T) (string, string) {
	t.Helper()
	origin := t.TempDir()
	run(t, origin, "git", "init", "--bare", "-b", "main")

	upstream := t.TempDir()
	run(t, upstream, "git", "init", "-b", "main")
	run(t, upstream, "git", "config", "user.email", "test@test.com")
	run(t, upstream, "git", "config", "user.name", "Test")
	run(t, upstream, "git", "commit", "--allow-empty", "-m", "init")
	run(t, upstream, "git", "remote", "add", "origin", origin)
	run(t, upstream, "git", "push", "origin", "main")

	clone := t.TempDir()
	run(t, clone, "git", "clone", origin, ".")
	run(t, clone, "git", "config", "user.email", "test@test.com")
	run(t, clone, "git", "config", "user.name", "Test")
	return clone, upstream
}

func TestGitCLI_ListAllBranches_DedupesLocalAndRemote(t *testing.T) {
	clone, upstream := initTestRepoWithOrigin(t)

	run(t, upstream, "git", "checkout", "-b", "feature-remote")
	run(t, upstream, "git", "commit", "--allow-empty", "-m", "feature commit")
	run(t, upstream, "git", "push", "origin", "feature-remote")

	run(t, clone, "git", "fetch", "origin")
	run(t, clone, "git", "branch", "local-only")

	svc := newGitCLI()
	refs, err := svc.listAllBranches(context.Background(), clone)
	require.NoError(t, err)

	byName := map[string]BranchRef{}
	for _, r := range refs {
		byName[r.Name] = r
	}
	require.Contains(t, byName, "main")
	require.False(t, byName["main"].Remote, "main exists locally")
	require.Contains(t, byName, "local-only")
	require.False(t, byName["local-only"].Remote)
	require.Contains(t, byName, "feature-remote")
	require.True(t, byName["feature-remote"].Remote, "feature-remote should be remote-only")
	require.Equal(t, "main", refs[0].Name, "main should be first")
}

func TestGitCLI_ListAllBranches_SkipsOriginHEAD(t *testing.T) {
	clone, _ := initTestRepoWithOrigin(t)

	svc := newGitCLI()
	refs, err := svc.listAllBranches(context.Background(), clone)
	require.NoError(t, err)
	for _, r := range refs {
		require.NotEqual(t, "HEAD", r.Name)
	}
}

func TestGitCLI_FetchOrigin_Succeeds(t *testing.T) {
	clone, upstream := initTestRepoWithOrigin(t)

	run(t, upstream, "git", "checkout", "-b", "new-from-upstream")
	run(t, upstream, "git", "commit", "--allow-empty", "-m", "upstream commit")
	run(t, upstream, "git", "push", "origin", "new-from-upstream")

	svc := newGitCLI()
	err := svc.fetchOrigin(context.Background(), clone)
	require.NoError(t, err)

	refs, err := svc.listAllBranches(context.Background(), clone)
	require.NoError(t, err)
	found := false
	for _, r := range refs {
		if r.Name == "new-from-upstream" {
			found = true
			require.True(t, r.Remote)
		}
	}
	require.True(t, found, "expected new-from-upstream after fetch")
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

func TestGitCLI_CloneBareWorkspace_HappyPath(t *testing.T) {
	source := initTestRepo(t)

	parent := t.TempDir()
	target := filepath.Join(parent, "cloned")

	svc := newGitCLI()
	err := svc.cloneBareWorkspace(context.Background(), source, target)
	require.NoError(t, err)

	gitInfo, err := os.Stat(filepath.Join(target, ".git"))
	require.NoError(t, err)
	require.False(t, gitInfo.IsDir(), ".git should be a file pointing at .bare")

	bareInfo, err := os.Stat(filepath.Join(target, ".bare"))
	require.NoError(t, err)
	require.True(t, bareInfo.IsDir(), ".bare should be a directory")

	contents, err := os.ReadFile(filepath.Join(target, ".git"))
	require.NoError(t, err)
	require.Equal(t, "gitdir: ./.bare\n", string(contents))

	cmd := exec.Command("git", "-C", target, "config", "--get", "remote.origin.fetch")
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "+refs/heads/*:refs/remotes/origin/*", trimOutput(out))

	require.True(t, isBareWorkspace(target))
}

func TestGitCLI_CloneBareWorkspace_CreatesParents(t *testing.T) {
	source := initTestRepo(t)

	parent := t.TempDir()
	target := filepath.Join(parent, "nested", "dirs", "cloned")

	svc := newGitCLI()
	err := svc.cloneBareWorkspace(context.Background(), source, target)
	require.NoError(t, err)

	require.True(t, isBareWorkspace(target))
}

func TestGitCLI_CloneBareWorkspace_RejectsNonEmptyTarget(t *testing.T) {
	source := initTestRepo(t)

	parent := t.TempDir()
	target := filepath.Join(parent, "cloned")
	require.NoError(t, os.MkdirAll(target, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(target, "stray"), []byte("x"), 0644))

	svc := newGitCLI()
	err := svc.cloneBareWorkspace(context.Background(), source, target)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not empty")

	_, statErr := os.Stat(filepath.Join(target, "stray"))
	require.NoError(t, statErr, "rejected clone must not delete user files")
}

func TestGitCLI_CloneBareWorkspace_RejectsEmptyURL(t *testing.T) {
	svc := newGitCLI()
	err := svc.cloneBareWorkspace(context.Background(), "", filepath.Join(t.TempDir(), "target"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "required")
}

func TestGitCLI_CloneBareWorkspace_CleansUpOnCloneFailure(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "cloned")

	svc := newGitCLI()
	err := svc.cloneBareWorkspace(context.Background(), filepath.Join(parent, "does-not-exist"), target)
	require.Error(t, err)

	_, statErr := os.Stat(target)
	require.True(t, os.IsNotExist(statErr), "failed clone should clean up its target dir, got: %v", statErr)
}
