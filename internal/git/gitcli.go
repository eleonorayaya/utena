package git

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type gitCLI struct{}

func newGitCLI() *gitCLI {
	return &gitCLI{}
}

type BranchRef struct {
	Name   string `json:"name"`
	Remote bool   `json:"remote"`
}

func (s *gitCLI) defaultBranch(ctx context.Context, repoPath string) string {
	ref := "refs/remotes/origin/HEAD"
	if isBareWorkspace(repoPath) {
		ref = "HEAD"
	}
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "symbolic-ref", ref)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	resolved := strings.TrimSpace(string(output))
	if name, ok := strings.CutPrefix(resolved, "refs/heads/"); ok {
		return name
	}
	if name, ok := strings.CutPrefix(resolved, "refs/remotes/origin/"); ok {
		return name
	}
	return ""
}

func (s *gitCLI) listBranches(ctx context.Context, repoPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--format=%(refname:short)")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	branches := []string{}
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			branches = append(branches, trimmed)
		}
	}

	def := s.defaultBranch(ctx, repoPath)

	if def != "" && !slices.Contains(branches, def) {
		branches = append([]string{def}, branches...)
	}

	priority := def
	if priority == "" {
		priority = "main"
	}
	for i, b := range branches {
		if b == priority {
			branches = append(branches[:i], branches[i+1:]...)
			return append([]string{priority}, branches...), nil
		}
	}

	return branches, nil
}

func (s *gitCLI) listAllBranches(ctx context.Context, repoPath string) ([]BranchRef, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "for-each-ref", "--format=%(refname:short)", "refs/heads", "refs/remotes/origin")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
	}

	refs := []BranchRef{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" {
			continue
		}
		name := ref
		remote := false
		if stripped, ok := strings.CutPrefix(ref, "origin/"); ok {
			if stripped == "HEAD" {
				continue
			}
			name = stripped
			remote = true
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		refs = append(refs, BranchRef{Name: name, Remote: remote})
	}

	if def := s.defaultBranch(ctx, repoPath); def != "" {
		for i, r := range refs {
			if r.Name == def {
				refs = append(refs[:i], refs[i+1:]...)
				refs = append([]BranchRef{r}, refs...)
				break
			}
		}
	}

	return refs, nil
}

func (s *gitCLI) fetchOrigin(ctx context.Context, repoPath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "--prune", "origin")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (s *gitCLI) pull(ctx context.Context, repoPath string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin", branch+":"+branch)
	if _, err := cmd.CombinedOutput(); err != nil {
		fallback := exec.CommandContext(ctx, "git", "-C", repoPath, "pull", "origin", branch)
		if output, err := fallback.CombinedOutput(); err != nil {
			return fmt.Errorf("git pull failed: %s: %w", string(output), err)
		}
	}
	return nil
}

func (s *gitCLI) fetch(ctx context.Context, repoPath string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin", branch+":"+branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (s *gitCLI) createWorktree(ctx context.Context, repoPath string, branchName string, baseBranch string) (string, error) {
	worktreePath := s.worktreePath(repoPath, branchName)
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "-b", branchName, worktreePath, baseBranch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}
	return worktreePath, nil
}

func (s *gitCLI) checkoutWorktree(ctx context.Context, repoPath string, branch string) (string, error) {
	worktreePath := s.worktreePath(repoPath, branch)
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", worktreePath, branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}
	return worktreePath, nil
}

func (s *gitCLI) currentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *gitCLI) isDirty(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("git status failed: %w", err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (s *gitCLI) hasRemoteBranch(ctx context.Context, repoPath string, branch string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("git ls-remote failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return strings.TrimSpace(string(output)) != "", nil
}

func (s *gitCLI) hasBranch(ctx context.Context, repoPath string, branch string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--verify", "refs/heads/"+branch)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *gitCLI) validateWorktree(ctx context.Context, worktreePath string, expectedBranch string) (bool, error) {
	info, err := os.Stat(worktreePath)
	if err != nil || !info.IsDir() {
		return false, nil
	}

	currentBranch, err := s.currentBranch(ctx, worktreePath)
	if err != nil {
		return false, fmt.Errorf("worktree exists at %s but failed to read branch: %w", worktreePath, err)
	}
	if currentBranch == "" {
		return true, nil
	}
	if currentBranch != expectedBranch {
		return false, fmt.Errorf("worktree at %s has branch %q, expected %q", worktreePath, currentBranch, expectedBranch)
	}
	return true, nil
}

func isBareWorkspace(path string) bool {
	gitInfo, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil || gitInfo.IsDir() {
		return false
	}
	bareInfo, err := os.Stat(filepath.Join(path, ".bare"))
	return err == nil && bareInfo.IsDir()
}

func (s *gitCLI) worktreePath(repoPath string, branch string) string {
	dirName := strings.ReplaceAll(branch, "/", "-")
	if isBareWorkspace(repoPath) {
		return filepath.Join(repoPath, dirName)
	}
	return filepath.Join(repoPath, ".worktrees", dirName)
}

func (s *gitCLI) removeWorktree(ctx context.Context, repoPath string, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "remove", worktreePath, "--force")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %s: %w", string(output), err)
	}
	return nil
}

func (s *gitCLI) deleteBranch(ctx context.Context, repoPath string, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "-D", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch delete failed: %s: %w", string(output), err)
	}
	return nil
}

func (s *gitCLI) remoteURL(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "remote", "get-url", "origin")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get remote URL: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *gitCLI) migrateToBare(ctx context.Context, workspacePath string) error {
	backupPath := workspacePath + "-backup"

	if _, err := os.Stat(backupPath); err == nil {
		return fmt.Errorf("backup directory already exists: %s", backupPath)
	}

	gitDir := filepath.Join(workspacePath, ".git")
	info, err := os.Stat(gitDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("workspace does not have a .git directory: %s", workspacePath)
	}

	remoteURL, err := s.remoteURL(ctx, workspacePath)
	if err != nil {
		return fmt.Errorf("failed to get remote URL (workspace must have a remote named 'origin'): %w", err)
	}

	if err := os.Rename(workspacePath, backupPath); err != nil {
		return fmt.Errorf("failed to move workspace to backup: %w", err)
	}

	if err := os.Mkdir(workspacePath, 0755); err != nil {
		return fmt.Errorf("failed to create workspace directory (backup at %s): %w", backupPath, err)
	}

	barePath := filepath.Join(workspacePath, ".bare")
	cmd := exec.CommandContext(ctx, "git", "clone", "--bare", remoteURL, barePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clone --bare failed (backup at %s): %s: %w", backupPath, strings.TrimSpace(string(output)), err)
	}

	gitFile := filepath.Join(workspacePath, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: ./.bare\n"), 0644); err != nil {
		return fmt.Errorf("failed to write .git file (backup at %s): %w", backupPath, err)
	}

	cmd = exec.CommandContext(ctx, "git", "-C", workspacePath, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set fetch refspec (backup at %s): %s: %w", backupPath, strings.TrimSpace(string(output)), err)
	}

	return nil
}

func (s *gitCLI) parseRepoFullName(remoteURL string) (owner string, repo string, err error) {
	remoteURL = strings.TrimSpace(remoteURL)

	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.SplitN(remoteURL, ":", 2)
		if len(parts) != 2 {
			return "", "", fmt.Errorf("invalid SSH remote URL: %s", remoteURL)
		}
		path := strings.TrimSuffix(parts[1], ".git")
		segments := strings.Split(path, "/")
		if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
			return "", "", fmt.Errorf("invalid SSH remote URL: %s", remoteURL)
		}
		return segments[0], segments[1], nil
	}

	parsed, parseErr := url.Parse(remoteURL)
	if parseErr != nil || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid remote URL: %s", remoteURL)
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	segments := strings.Split(path, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("invalid remote URL path: %s", remoteURL)
	}

	return segments[0], segments[1], nil
}
