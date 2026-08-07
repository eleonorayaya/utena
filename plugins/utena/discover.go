package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	manifestName  = ".utena-session.json"
	legacyMarker  = "utena:multi-session-guide"
	manifestOwner = "utena"
)

type manifest struct {
	Owner     string    `json:"owner"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	Archived  bool      `json:"archived,omitempty"`
	Repos     []string  `json:"repos"`
}

func writeManifest(sessionRoot string, m manifest) error {
	m.Owner = manifestOwner
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	path := filepath.Join(sessionRoot, manifestName)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

func readManifest(sessionRoot string) (manifest, bool) {
	data, err := os.ReadFile(filepath.Join(sessionRoot, manifestName))
	if err != nil {
		return manifest{}, false
	}
	var m manifest
	if json.Unmarshal(data, &m) != nil {
		return manifest{}, false
	}
	return m, true
}

type Checkout struct {
	Label       string
	Path        string
	Repo        string
	Branch      string
	Dirty       int
	WorkspaceID string
	AgentStatus string
}

type Session struct {
	Name        string
	Root        string
	Archived    bool
	Checkouts   []Checkout
	WorkspaceID string
	AgentStatus string
}

func (s Session) Active() bool { return s.WorkspaceID != "" }

func sessionRoots() []string {
	if raw := os.Getenv("UTENA_SESSION_ROOTS"); raw != "" {
		return filepath.SplitList(raw)
	}
	root, err := sessionsRoot()
	if err != nil {
		return nil
	}
	roots := []string{root}
	if home, err := os.UserHomeDir(); err == nil {
		if legacy := filepath.Join(home, "utena-sessions"); legacy != root {
			if _, err := os.Stat(legacy); err == nil {
				roots = append(roots, legacy)
			}
		}
	}
	return roots
}

func isSessionRoot(dir string) bool {
	if _, ok := readManifest(dir); ok {
		return true
	}
	data, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), legacyMarker)
}

func worktreeInfo(checkout string) (repo, branch string, ok bool) {
	data, err := os.ReadFile(filepath.Join(checkout, ".git"))
	if err != nil {
		return "", "", false
	}
	gitdir := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if gitdir == "" {
		return "", "", false
	}
	if !filepath.IsAbs(gitdir) {
		gitdir = filepath.Join(checkout, gitdir)
	}

	common := gitdir
	if rel, err := os.ReadFile(filepath.Join(gitdir, "commondir")); err == nil {
		common = filepath.Clean(filepath.Join(gitdir, strings.TrimSpace(string(rel))))
	}
	repo = filepath.Dir(common)

	head, err := os.ReadFile(filepath.Join(gitdir, "HEAD"))
	if err != nil {
		return repo, "", true
	}
	branch = strings.TrimSpace(string(head))
	if ref, found := strings.CutPrefix(branch, "ref: refs/heads/"); found {
		branch = ref
	} else if len(branch) > 8 {
		branch = "detached " + branch[:8]
	}
	return repo, branch, true
}

func scanCheckouts(sessionDir string) []Checkout {
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil
	}
	var out []Checkout
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		path := filepath.Join(sessionDir, e.Name())
		repo, branch, ok := worktreeInfo(path)
		if !ok {
			continue
		}
		out = append(out, Checkout{
			Label:  e.Name(),
			Path:   path,
			Repo:   repo,
			Branch: branch,
		})
	}
	return out
}

func scanSessions() []Session {
	var sessions []Session
	seen := map[string]struct{}{}
	for _, root := range sessionRoots() {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, e.Name())
			if _, dup := seen[dir]; dup || !isSessionRoot(dir) {
				continue
			}
			seen[dir] = struct{}{}
			m, _ := readManifest(dir)
			sessions = append(sessions, Session{
				Name:      e.Name(),
				Root:      dir,
				Archived:  m.Archived,
				Checkouts: scanCheckouts(dir),
			})
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].Name < sessions[j].Name })
	return sessions
}

func resolve(path string) string {
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}

type liveWorkspace struct {
	ID          string
	Label       string
	AgentStatus string
	Cwd         string
}

func indexLive(h *herdrClient) (byCwd map[string]liveWorkspace, all []liveWorkspace, err error) {
	snap, err := h.snapshot()
	if err != nil {
		return nil, nil, err
	}
	ws, err := h.listWorkspaces()
	if err != nil {
		return nil, nil, err
	}

	cwdOf := map[string]string{}
	for _, p := range snap.Panes {
		if _, ok := cwdOf[p.WorkspaceID]; !ok {
			cwdOf[p.WorkspaceID] = p.Cwd
		}
	}

	byCwd = make(map[string]liveWorkspace, len(ws))
	for id, w := range ws {
		lw := liveWorkspace{ID: id, Label: w.Label, AgentStatus: w.AgentStatus, Cwd: cwdOf[id]}
		all = append(all, lw)
		if lw.Cwd != "" {
			byCwd[resolve(lw.Cwd)] = lw
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Label < all[j].Label })
	return byCwd, all, nil
}

func loadSessions(h *herdrClient) ([]Session, []liveWorkspace, error) {
	byCwd, all, err := indexLive(h)
	if err != nil {
		return nil, nil, err
	}

	sessions := scanSessions()
	claimed := map[string]struct{}{}

	for i := range sessions {
		s := &sessions[i]
		if lw, ok := byCwd[resolve(s.Root)]; ok {
			s.WorkspaceID, s.AgentStatus = lw.ID, lw.AgentStatus
			claimed[lw.ID] = struct{}{}
		}
		for j := range s.Checkouts {
			c := &s.Checkouts[j]
			if lw, ok := byCwd[resolve(c.Path)]; ok {
				c.WorkspaceID, c.AgentStatus = lw.ID, lw.AgentStatus
				claimed[lw.ID] = struct{}{}
			}
		}
	}

	var ungrouped []liveWorkspace
	for _, lw := range all {
		if _, ok := claimed[lw.ID]; !ok {
			ungrouped = append(ungrouped, lw)
		}
	}
	return sessions, ungrouped, nil
}

func setArchived(sessionRoot string, archived bool) error {
	m, ok := readManifest(sessionRoot)
	if !ok {
		m = manifest{Name: filepath.Base(sessionRoot), CreatedAt: time.Now()}
	}
	m.Archived = archived
	return writeManifest(sessionRoot, m)
}
