package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/claudesettings"
)

var (
	invalidCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
	multiDash           = regexp.MustCompile(`-{2,}`)
)

func sanitizeName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = invalidCharsPattern.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	if s == "" {
		return "session"
	}
	return s
}

func sessionsRoot() (string, error) {
	if root := os.Getenv("HERDR_UTENA_SESSIONS_ROOT"); root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, "herdr-sessions"), nil
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func branchExists(repo, branch string) bool {
	_, err := git(repo, "rev-parse", "--verify", "refs/heads/"+branch)
	return err == nil
}

func worktreeAt(repo, path string) bool {
	out, err := git(repo, "worktree", "list", "--porcelain")
	if err != nil {
		return false
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		target = path
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "worktree ") {
			continue
		}
		existing := strings.TrimPrefix(line, "worktree ")
		if resolved, err := filepath.EvalSymlinks(existing); err == nil {
			existing = resolved
		}
		if existing == target {
			return true
		}
	}
	return false
}

func addWorktree(repo, path, branch string) error {
	if worktreeAt(repo, path) {
		return nil
	}
	if branchExists(repo, branch) {
		_, err := git(repo, "worktree", "add", path, branch)
		return err
	}
	_, err := git(repo, "worktree", "add", "-b", branch, path)
	return err
}

func archiveSession(h *herdrClient, name string) error {
	state, err := loadState()
	if err != nil {
		return err
	}
	sess, ok := state.find(name)
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}
	for _, c := range sess.Checkouts {
		if c.WorkspaceID != "" {
			_ = h.closeWorkspace(c.WorkspaceID)
		}
	}
	if sess.WorkspaceID != "" {
		_ = h.closeWorkspace(sess.WorkspaceID)
	}
	sess.Status = statusArchived
	sess.LastUsedAt = time.Now()
	return saveState(state)
}

func deleteSession(h *herdrClient, name string) error {
	state, err := loadState()
	if err != nil {
		return err
	}
	sess, ok := state.find(name)
	if !ok {
		return fmt.Errorf("session %q not found", name)
	}
	for _, c := range sess.Checkouts {
		if c.WorkspaceID != "" {
			_ = h.closeWorkspace(c.WorkspaceID)
		}
		if _, err := git(c.Repo, "worktree", "remove", c.Path, "--force"); err != nil {
			return fmt.Errorf("remove worktree %s: %w", c.Label, err)
		}
	}
	if sess.WorkspaceID != "" {
		_ = h.closeWorkspace(sess.WorkspaceID)
	}
	if err := os.RemoveAll(sess.Root); err != nil {
		return fmt.Errorf("remove session root: %w", err)
	}
	kept := state.Sessions[:0]
	for _, s := range state.Sessions {
		if s.Name != name {
			kept = append(kept, s)
		}
	}
	state.Sessions = kept
	return saveState(state)
}

type createInput struct {
	Name   string
	Branch string
	Repos  []string
}

func createSession(h *herdrClient, in createInput) (*Session, error) {
	if in.Branch == "" {
		return nil, fmt.Errorf("branch is required")
	}
	if len(in.Repos) == 0 {
		return nil, fmt.Errorf("at least one -repo is required")
	}

	name := sanitizeName(in.Name)
	if in.Name == "" {
		name = sanitizeName(in.Branch)
	}

	root, err := sessionsRoot()
	if err != nil {
		return nil, err
	}
	sessionRoot := filepath.Join(root, name)

	state, err := loadState()
	if err != nil {
		return nil, err
	}
	if existing, ok := state.find(name); ok && existing.Status != statusArchived {
		return nil, fmt.Errorf("session %q already exists at %s", name, existing.Root)
	}

	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create session root: %w", err)
	}

	sess := Session{
		Name:       name,
		Root:       sessionRoot,
		Status:     statusActive,
		CreatedAt:  time.Now(),
		LastUsedAt: time.Now(),
	}

	repoPaths := make([]string, 0, len(in.Repos))
	guide := make([]claudesettings.SessionCheckout, 0, len(in.Repos))

	for _, repo := range in.Repos {
		abs, err := filepath.Abs(repo)
		if err != nil {
			return nil, fmt.Errorf("resolve repo %s: %w", repo, err)
		}
		if _, err := git(abs, "rev-parse", "--git-dir"); err != nil {
			return nil, fmt.Errorf("%s is not a git repository: %w", abs, err)
		}

		label := filepath.Base(abs)
		checkout := filepath.Join(sessionRoot, label)

		if err := addWorktree(abs, checkout, in.Branch); err != nil {
			return nil, fmt.Errorf("worktree for %s: %w", label, err)
		}

		wsID, err := h.createWorkspace(checkout, label)
		if err != nil {
			return nil, fmt.Errorf("register workspace for %s: %w", label, err)
		}

		repoPaths = append(repoPaths, abs)
		guide = append(guide, claudesettings.SessionCheckout{Subdir: label, WorkspaceName: label})
		sess.Checkouts = append(sess.Checkouts, Checkout{
			Repo:        abs,
			Label:       label,
			Path:        checkout,
			Branch:      in.Branch,
			WorkspaceID: wsID,
		})
	}

	if err := claudesettings.EnsureSessionRoot(sessionRoot, repoPaths); err != nil {
		return nil, fmt.Errorf("claude settings: %w", err)
	}
	if err := claudesettings.EnsureMultiSessionGuide(sessionRoot, guide); err != nil {
		return nil, fmt.Errorf("session guide: %w", err)
	}

	wsID, err := h.createWorkspace(sessionRoot, name)
	if err != nil {
		return nil, fmt.Errorf("create session workspace: %w", err)
	}
	sess.WorkspaceID = wsID

	group := []string{wsID}
	for _, c := range sess.Checkouts {
		group = append(group, c.WorkspaceID)
	}
	for _, id := range group {
		if err := h.tagSession(id, name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: tag %s: %v\n", id, err)
		}
	}
	if err := h.groupWorkspaces(group); err != nil {
		fmt.Fprintf(os.Stderr, "warning: group workspaces: %v\n", err)
	}

	state.upsert(sess)
	if err := saveState(state); err != nil {
		return nil, err
	}
	return &sess, nil
}
