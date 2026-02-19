package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/afero"
)

type WorkspaceNotFoundError struct {
	WorkspaceID string
}

func (e *WorkspaceNotFoundError) Error() string {
	return "workspace not found: " + e.WorkspaceID
}

type config struct {
	WorkspaceRoots []string `json:"workspace_roots"`
	Workspaces     []string `json:"workspaces,omitempty"`
}

type WorkspaceStore struct {
	mu         sync.RWMutex
	workspaces map[string]*Workspace
	fs         afero.Fs
	configDir  string
}

func NewWorkspaceStore(fs afero.Fs, configDir string) *WorkspaceStore {
	return &WorkspaceStore{
		workspaces: make(map[string]*Workspace),
		fs:         fs,
		configDir:  configDir,
	}
}

func (s *WorkspaceStore) configPath() string {
	return filepath.Join(s.configDir, "config.json")
}

func (s *WorkspaceStore) GetByID(id string) (*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ws, ok := s.workspaces[id]
	if !ok {
		return nil, &WorkspaceNotFoundError{WorkspaceID: id}
	}

	return ws, nil
}

func (s *WorkspaceStore) GetByPath(path string) (*Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ws := range s.workspaces {
		if ws.Path == path {
			return ws, nil
		}
	}

	return nil, &WorkspaceNotFoundError{WorkspaceID: path}
}

func (s *WorkspaceStore) List() []Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()

	workspaces := make([]Workspace, 0, len(s.workspaces))
	for _, ws := range s.workspaces {
		workspaces = append(workspaces, *ws)
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})

	return workspaces
}

func (s *WorkspaceStore) Add(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace cannot be nil")
	}

	if ws.ID == "" {
		return errors.New("workspace ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.workspaces[ws.ID]; exists {
		return errors.New("workspace with this ID already exists")
	}

	s.workspaces[ws.ID] = ws
	return nil
}

func (s *WorkspaceStore) AddWorkspace(path string) (*Workspace, error) {
	info, err := s.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	id := generateID(path)

	s.mu.RLock()
	_, exists := s.workspaces[id]
	s.mu.RUnlock()
	if exists {
		return nil, fmt.Errorf("workspace already exists: %s", path)
	}

	ws := &Workspace{
		ID:        id,
		Name:      filepath.Base(path),
		Path:      path,
		IsGitRepo: s.isGitRepository(path),
	}

	if err := s.Add(ws); err != nil {
		return nil, err
	}

	cfg, err := s.loadConfig()
	if err != nil {
		cfg = &config{}
	}
	cfg.Workspaces = append(cfg.Workspaces, path)
	if err := s.saveConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return ws, nil
}

func (s *WorkspaceStore) AddWorkspaceRoot(path string) ([]Workspace, error) {
	info, err := s.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	entries, err := afero.ReadDir(s.fs, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	var added []Workspace
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(path, entry.Name())
		id := generateID(fullPath)

		s.mu.RLock()
		_, exists := s.workspaces[id]
		s.mu.RUnlock()
		if exists {
			continue
		}

		ws := &Workspace{
			ID:        id,
			Name:      entry.Name(),
			Path:      fullPath,
			IsGitRepo: s.isGitRepository(fullPath),
		}
		if err := s.Add(ws); err != nil {
			continue
		}
		added = append(added, *ws)
	}

	cfg, err := s.loadConfig()
	if err != nil {
		cfg = &config{}
	}
	cfg.WorkspaceRoots = append(cfg.WorkspaceRoots, path)
	if err := s.saveConfig(cfg); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return added, nil
}

func (s *WorkspaceStore) OnAppStart(ctx context.Context) error {
	workspaces, err := s.discoverWorkspaces()
	if err != nil {
		return err
	}

	for _, ws := range workspaces {
		if err := s.Add(ws); err != nil {
			return err
		}
	}

	return nil
}

func (s *WorkspaceStore) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *WorkspaceStore) discoverWorkspaces() ([]*Workspace, error) {
	cfg, err := s.loadConfig()
	if err != nil {
		slog.Warn("failed to load workspace config, no workspaces will be available", "error", err)
		return nil, nil
	}

	if len(cfg.WorkspaceRoots) == 0 && len(cfg.Workspaces) == 0 {
		return nil, nil
	}

	var workspaces []*Workspace

	for _, root := range cfg.WorkspaceRoots {
		expanded := expandHome(root)

		entries, err := afero.ReadDir(s.fs, expanded)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			fullPath := filepath.Join(expanded, entry.Name())
			id := generateID(fullPath)
			isGitRepo := s.isGitRepository(fullPath)

			workspaces = append(workspaces, &Workspace{
				ID:        id,
				Name:      entry.Name(),
				Path:      fullPath,
				IsGitRepo: isGitRepo,
			})
		}
	}

	for _, wsPath := range cfg.Workspaces {
		expanded := expandHome(wsPath)
		info, err := s.fs.Stat(expanded)
		if err != nil || !info.IsDir() {
			continue
		}
		id := generateID(expanded)
		isGitRepo := s.isGitRepository(expanded)
		workspaces = append(workspaces, &Workspace{
			ID:        id,
			Name:      filepath.Base(expanded),
			Path:      expanded,
			IsGitRepo: isGitRepo,
		})
	}

	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})

	return workspaces, nil
}

func (s *WorkspaceStore) loadConfig() (*config, error) {
	data, err := afero.ReadFile(s.fs, s.configPath())
	if err != nil {
		return nil, err
	}

	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (s *WorkspaceStore) saveConfig(cfg *config) error {
	if err := s.fs.MkdirAll(filepath.Dir(s.configPath()), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return afero.WriteFile(s.fs, s.configPath(), data, 0644)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

func generateID(path string) string {
	hash := sha256.Sum256([]byte(path))
	return hex.EncodeToString(hash[:8])
}

func (s *WorkspaceStore) isGitRepository(path string) bool {
	info, err := s.fs.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}
