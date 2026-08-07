package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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

func repoRoots() []string {
	if raw := os.Getenv("UTENA_REPO_ROOTS"); raw != "" {
		return filepath.SplitList(raw)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "workspace")}
}

func discoverRepos() []string {
	var out []string
	seen := map[string]struct{}{}
	for _, root := range repoRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(root, e.Name())
			if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
				continue
			}
			if _, dup := seen[path]; dup {
				continue
			}
			seen[path] = struct{}{}
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

func sessionsRoot() (string, error) {
	if root := os.Getenv("UTENA_SESSIONS_ROOT"); root != "" {
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

func findSession(name string) (Session, bool) {
	for _, s := range scanSessions() {
		if s.Name == name {
			return s, true
		}
	}
	return Session{}, false
}

func registerSession(h *herdrClient, s Session) (Session, error) {
	for i := range s.Checkouts {
		c := &s.Checkouts[i]
		if c.WorkspaceID != "" {
			continue
		}
		id, err := h.createWorkspace(c.Path, c.Label)
		if err != nil {
			return s, fmt.Errorf("register workspace for %s: %w", c.Label, err)
		}
		c.WorkspaceID = id
	}
	if s.WorkspaceID == "" {
		id, err := h.createWorkspace(s.Root, s.Name)
		if err != nil {
			return s, fmt.Errorf("create session workspace: %w", err)
		}
		s.WorkspaceID = id
	}

	group := []string{s.WorkspaceID}
	for _, c := range s.Checkouts {
		group = append(group, c.WorkspaceID)
	}
	for _, id := range group {
		if err := h.tagSession(id, s.Name); err != nil {
			fmt.Fprintf(os.Stderr, "warning: tag %s: %v\n", id, err)
		}
	}
	if err := h.groupWorkspaces(group); err != nil {
		fmt.Fprintf(os.Stderr, "warning: group workspaces: %v\n", err)
	}
	return s, nil
}

func activateSession(h *herdrClient, name string) error {
	sessions, _, err := loadSessions(h)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.Name != name {
			continue
		}
		if _, err := registerSession(h, s); err != nil {
			return err
		}
		return setArchived(s.Root, false)
	}
	return fmt.Errorf("session %q not found", name)
}

func archiveSession(h *herdrClient, name string) error {
	sessions, _, err := loadSessions(h)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.Name != name {
			continue
		}
		for _, c := range s.Checkouts {
			if c.WorkspaceID != "" {
				_ = h.closeWorkspace(c.WorkspaceID)
			}
		}
		if s.WorkspaceID != "" {
			_ = h.closeWorkspace(s.WorkspaceID)
		}
		return setArchived(s.Root, true)
	}
	return fmt.Errorf("session %q not found", name)
}

func unarchiveSession(h *herdrClient, name string) error {
	for _, s := range scanSessions() {
		if s.Name == name {
			return setArchived(s.Root, false)
		}
	}
	return fmt.Errorf("session %q not found", name)
}

func deleteSession(h *herdrClient, name string) error {
	sessions, _, err := loadSessions(h)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.Name != name {
			continue
		}
		for _, c := range s.Checkouts {
			if c.WorkspaceID != "" {
				_ = h.closeWorkspace(c.WorkspaceID)
			}
			if c.Repo != "" {
				if _, err := git(c.Repo, "worktree", "remove", c.Path, "--force"); err != nil {
					return fmt.Errorf("remove worktree %s: %w", c.Label, err)
				}
			}
		}
		if s.WorkspaceID != "" {
			_ = h.closeWorkspace(s.WorkspaceID)
		}
		if err := os.RemoveAll(s.Root); err != nil {
			return fmt.Errorf("remove session root: %w", err)
		}
		return nil
	}
	return fmt.Errorf("session %q not found", name)
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
		return nil, fmt.Errorf("at least one repo is required")
	}

	name := sanitizeName(in.Name)
	if in.Name == "" {
		name = sanitizeName(in.Branch)
	}
	if _, exists := findSession(name); exists {
		return nil, fmt.Errorf("session %q already exists", name)
	}

	root, err := sessionsRoot()
	if err != nil {
		return nil, err
	}
	sessionRoot := filepath.Join(root, name)
	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create session root: %w", err)
	}

	sess := Session{Name: name, Root: sessionRoot}
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
		repoPaths = append(repoPaths, abs)
		guide = append(guide, claudesettings.SessionCheckout{Subdir: label, WorkspaceName: label})
		sess.Checkouts = append(sess.Checkouts, Checkout{
			Repo: abs, Label: label, Path: checkout, Branch: in.Branch,
		})
	}

	if err := claudesettings.EnsureSessionRoot(sessionRoot, repoPaths); err != nil {
		return nil, fmt.Errorf("claude settings: %w", err)
	}
	if err := claudesettings.EnsureMultiSessionGuide(sessionRoot, guide); err != nil {
		return nil, fmt.Errorf("session guide: %w", err)
	}
	if err := writeManifest(sessionRoot, manifest{
		Name: name, CreatedAt: time.Now(), Repos: repoPaths,
	}); err != nil {
		return nil, err
	}

	sess, err = registerSession(h, sess)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}
