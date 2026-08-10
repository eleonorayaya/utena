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
	if roots := loadConfig().RepoRoots; len(roots) > 0 {
		return roots
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{filepath.Join(home, "workspace")}
}

func isGitRepo(path string) bool {
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		return true
	}
	_, err := os.Stat(filepath.Join(path, ".bare"))
	return err == nil
}

func discoverRepos() []string {
	var out []string
	seen := map[string]struct{}{}
	for _, path := range loadConfig().Repos {
		if _, dup := seen[path]; dup || !isGitRepo(path) {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
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
			if !isGitRepo(path) {
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
	if roots := loadConfig().SessionRoots; len(roots) > 0 {
		return roots[0], nil
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

func addWorktree(repo, path, branch, base string) error {
	if worktreeAt(repo, path) {
		return nil
	}
	if base != "" && !branchExists(repo, branch) {
		_, err := git(repo, "worktree", "add", "-b", branch, path, base)
		return err
	}
	if branchExists(repo, branch) {
		_, err := git(repo, "worktree", "add", path, branch)
		return err
	}
	_, err := git(repo, "worktree", "add", "-b", branch, path)
	return err
}

func localBranches(repo string) []string {
	out, err := git(repo, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)", "refs/heads")
	if err != nil || strings.TrimSpace(out) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(out), "\n")
}

func remoteBranches(repo string) []string {
	out, err := git(repo, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname:short)", "refs/remotes")
	if err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var names []string
	for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasSuffix(l, "/HEAD") {
			continue
		}
		if _, remote, ok := strings.Cut(l, "/"); ok {
			if _, dup := seen[remote]; dup {
				continue
			}
			seen[remote] = struct{}{}
			names = append(names, remote)
		}
	}
	return names
}

func fetchOrigin(repo string) error {
	_, err := git(repo, "fetch", "--quiet", "origin")
	return err
}

func branchExistsAnywhere(repo, name string) (local, remote bool) {
	local = branchExists(repo, name)
	if out, err := git(repo, "ls-remote", "--heads", "origin", name); err == nil {
		remote = strings.TrimSpace(out) != ""
	}
	return local, remote
}

func defaultBranch(repo string) string {
	if out, err := git(repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		if _, b, ok := strings.Cut(strings.TrimSpace(out), "/"); ok {
			return b
		}
	}
	for _, c := range []string{"main", "master"} {
		if branchExists(repo, c) {
			return c
		}
	}
	return ""
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
		touchSession(s.Root)
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
		}
		if s.WorkspaceID != "" {
			_ = h.closeWorkspace(s.WorkspaceID)
		}
		return markDeleted(s.Root)
	}
	return fmt.Errorf("session %q not found", name)
}

// checkoutSpec mirrors utena's SessionWorkspaceSpec: Branch alone checks out an
// existing branch, Base additionally creates Branch from it.
type checkoutSpec struct {
	Repo   string
	Branch string
	Base   string
}

type createInput struct {
	Name      string
	Checkouts []checkoutSpec
}

func createSession(h *herdrClient, in createInput) (*Session, error) {
	if len(in.Checkouts) == 0 {
		return nil, fmt.Errorf("at least one repo is required")
	}
	for _, c := range in.Checkouts {
		if c.Branch == "" {
			return nil, fmt.Errorf("branch is required for %s", filepath.Base(c.Repo))
		}
	}

	name := sanitizeName(in.Name)
	if in.Name == "" {
		name = sanitizeName(in.Checkouts[0].Branch)
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
	repoPaths := make([]string, 0, len(in.Checkouts))
	guide := make([]claudesettings.SessionCheckout, 0, len(in.Checkouts))

	for _, spec := range in.Checkouts {
		abs, err := filepath.Abs(spec.Repo)
		if err != nil {
			return nil, fmt.Errorf("resolve repo %s: %w", spec.Repo, err)
		}
		if _, err := git(abs, "rev-parse", "--git-dir"); err != nil {
			return nil, fmt.Errorf("%s is not a git repository: %w", abs, err)
		}
		label := filepath.Base(abs)
		checkout := filepath.Join(sessionRoot, label)
		if err := addWorktree(abs, checkout, spec.Branch, spec.Base); err != nil {
			return nil, fmt.Errorf("worktree for %s: %w", label, err)
		}
		repoPaths = append(repoPaths, abs)
		guide = append(guide, claudesettings.SessionCheckout{Subdir: label, WorkspaceName: label})
		sess.Checkouts = append(sess.Checkouts, Checkout{
			Repo: abs, Label: label, Path: checkout, Branch: spec.Branch,
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
