package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
	configPath string
}

func NewWorkspaceStore() *WorkspaceStore {
	homeDir, _ := os.UserHomeDir()
	return &WorkspaceStore{
		workspaces: make(map[string]*Workspace),
		configPath: filepath.Join(homeDir, ".config", "utena", "config.json"),
	}
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
		return nil, nil
	}

	if len(cfg.WorkspaceRoots) == 0 && len(cfg.Workspaces) == 0 {
		return nil, nil
	}

	var workspaces []*Workspace

	for _, root := range cfg.WorkspaceRoots {
		expanded := expandHome(root)

		entries, err := os.ReadDir(expanded)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			fullPath := filepath.Join(expanded, entry.Name())
			id := generateID(fullPath)
			isGitRepo := isGitRepository(fullPath)

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
		info, err := os.Stat(expanded)
		if err != nil || !info.IsDir() {
			continue
		}
		id := generateID(expanded)
		isGitRepo := isGitRepository(expanded)
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
	data, err := os.ReadFile(s.configPath)
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
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath, data, 0644)
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

func isGitRepository(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false
	}
	return info.IsDir()
}
