package git

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type GitService struct{}

func NewGitService() *GitService {
	return &GitService{}
}

func (s *GitService) ListBranches(ctx context.Context, repoPath string) ([]string, error) {
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

	for i, b := range branches {
		if b == "main" {
			branches = append(branches[:i], branches[i+1:]...)
			branches = append([]string{"main"}, branches...)
			break
		}
	}

	return branches, nil
}

func (s *GitService) Pull(ctx context.Context, repoPath string, branch string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "fetch", "origin", branch+":"+branch)
	if _, err := cmd.CombinedOutput(); err != nil {
		fallback := exec.CommandContext(ctx, "git", "-C", repoPath, "pull", "origin", branch)
		if output, err := fallback.CombinedOutput(); err != nil {
			return fmt.Errorf("git pull failed: %s: %w", string(output), err)
		}
	}
	return nil
}

func (s *GitService) CreateWorktree(ctx context.Context, repoPath string, name string, baseBranch string) (string, error) {
	worktreePath := filepath.Join(repoPath, ".worktrees", name)
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "-b", name, worktreePath, baseBranch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}
	return worktreePath, nil
}

func (s *GitService) CheckoutWorktree(ctx context.Context, repoPath string, branch string) (string, error) {
	sanitized := strings.ReplaceAll(branch, "/", "-")
	worktreePath := filepath.Join(repoPath, ".worktrees", sanitized)
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", worktreePath, branch)
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git worktree add failed: %s: %w", string(output), err)
	}
	return worktreePath, nil
}

func (s *GitService) CurrentBranch(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "--show-current")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func (s *GitService) RemoveWorktree(ctx context.Context, repoPath string, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "worktree", "remove", worktreePath, "--force")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git worktree remove failed: %s: %w", string(output), err)
	}
	return nil
}

func (s *GitService) DeleteBranch(ctx context.Context, repoPath string, branchName string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "branch", "-D", branchName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git branch delete failed: %s: %w", string(output), err)
	}
	return nil
}
