package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/spf13/afero"
	"gorm.io/gorm"
)

type config struct {
	WorkspaceRoots []string `json:"workspace_roots"`
	Workspaces     []string `json:"workspaces,omitempty"`
}

type WorkspaceStore struct {
	db        db.Database
	fs        afero.Fs
	configDir string
}

func NewWorkspaceStore(database db.Database, fs afero.Fs, configDir string) *WorkspaceStore {
	return &WorkspaceStore{
		db:        database,
		fs:        fs,
		configDir: configDir,
	}
}

func (s *WorkspaceStore) configPath() string {
	return filepath.Join(s.configDir, "config.json")
}

func (s *WorkspaceStore) GetByID(id uint) (*Workspace, error) {
	var ws Workspace
	if err := s.db.Joins("Repo").First(&ws, "workspaces.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound(fmt.Sprintf("workspace not found: %d", id))
		}
		return nil, err
	}
	return &ws, nil
}

func (s *WorkspaceStore) GetByPath(path string) (*Workspace, error) {
	var ws Workspace
	if err := s.db.Joins("Repo").First(&ws, "workspaces.path = ?", path).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound("workspace not found: " + path)
		}
		return nil, err
	}
	return &ws, nil
}

func (s *WorkspaceStore) GetByRepoID(repoID uint) (*Workspace, error) {
	var ws Workspace
	if err := s.db.Joins("Repo").First(&ws, "workspaces.repo_id = ?", repoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, common.NewNotFound(fmt.Sprintf("workspace not found for repo: %d", repoID))
		}
		return nil, err
	}
	return &ws, nil
}

func (s *WorkspaceStore) List() []Workspace {
	var workspaces []Workspace
	s.db.Joins("Repo").Find(&workspaces)

	sort.Slice(workspaces, func(i, j int) bool {
		iUsed := !workspaces[i].LastUsedAt.IsZero()
		jUsed := !workspaces[j].LastUsedAt.IsZero()

		if iUsed && jUsed {
			return workspaces[i].LastUsedAt.After(workspaces[j].LastUsedAt)
		}
		if iUsed != jUsed {
			return iUsed
		}
		return workspaces[i].Name < workspaces[j].Name
	})

	return workspaces
}

func (s *WorkspaceStore) Add(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace cannot be nil")
	}

	if err := s.db.Omit("Repo").Create(ws).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.NewConflict(workspaceConflictMessage(ws, err))
		}
		return err
	}
	return nil
}

func (s *WorkspaceStore) Update(ws *Workspace) error {
	if ws == nil {
		return errors.New("workspace cannot be nil")
	}
	if ws.ID == 0 {
		return errors.New("workspace ID cannot be zero")
	}

	var existing Workspace
	if err := s.db.First(&existing, "id = ?", ws.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFound(fmt.Sprintf("workspace not found: %d", ws.ID))
		}
		return err
	}

	if err := s.db.Omit("Repo").Save(ws).Error; err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || db.IsUniqueConstraintError(err) {
			return common.NewConflict(workspaceConflictMessage(ws, err))
		}
		return err
	}
	return nil
}

// workspaceConflictMessage produces a friendly message for a unique-constraint
// violation on the Workspace table. SQLite reports the offending column; we
// surface either the path or the repo as the conflicting concept along with
// a hint for what to do next.
func workspaceConflictMessage(ws *Workspace, err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "workspaces.repo_id"):
		return "another workspace already tracks this repo — remove it first or use a different repo"
	case strings.Contains(msg, "workspaces.path"):
		return fmt.Sprintf("a workspace at path %q already exists — remove it first or pick a different path", ws.Path)
	default:
		return fmt.Sprintf("workspace %q already exists", ws.Name)
	}
}

func (s *WorkspaceStore) SetHidden(id uint, hidden bool) error {
	var ws Workspace
	if err := s.db.First(&ws, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFound(fmt.Sprintf("workspace not found: %d", id))
		}
		return err
	}

	return s.db.Model(&ws).Update("is_hidden", hidden).Error
}

func (s *WorkspaceStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("workspace ID cannot be zero")
	}

	var existing Workspace
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return common.NewNotFound(fmt.Sprintf("workspace not found: %d", id))
		}
		return err
	}

	return s.db.Where("id = ?", id).Unscoped().Delete(&Workspace{}).Error
}

func (s *WorkspaceStore) RemoveWorkspaceFromConfig(path string) {
	cfg, err := s.loadConfig()
	if err != nil {
		return
	}

	filtered := make([]string, 0, len(cfg.Workspaces))
	for _, ws := range cfg.Workspaces {
		if expandHome(ws) != expandHome(path) {
			filtered = append(filtered, ws)
		}
	}

	if len(filtered) != len(cfg.Workspaces) {
		cfg.Workspaces = filtered
		if err := s.saveConfig(cfg); err != nil {
			slog.Warn("failed to persist workspace config after removal", "path", path, "error", err)
		}
	}
}

func (s *WorkspaceStore) AddWorkspace(path string) (*Workspace, error) {
	info, err := s.fs.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("invalid path: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("path is not a directory: %s", path)
	}

	var existing Workspace
	if err := s.db.First(&existing, "path = ?", path).Error; err == nil {
		return nil, common.NewConflict(fmt.Sprintf("workspace at path %q already exists — remove it first or pick a different path", path))
	}

	isGit, isBare := s.detectRepoKind(path)
	ws := &Workspace{
		Name:      filepath.Base(path),
		Path:      path,
		IsGitRepo: isGit,
		IsBare:    isBare,
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

		var existing Workspace
		if err := s.db.First(&existing, "path = ?", fullPath).Error; err == nil {
			continue
		}

		isGit, isBare := s.detectRepoKind(fullPath)
		ws := &Workspace{
			Name:      entry.Name(),
			Path:      fullPath,
			IsGitRepo: isGit,
			IsBare:    isBare,
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
	discovered, err := s.discoverWorkspaces()
	if err != nil {
		return err
	}

	for _, ws := range discovered {
		var existing Workspace
		if err := s.db.First(&existing, "path = ?", ws.Path).Error; err == nil {
			existing.Name = ws.Name
			existing.IsGitRepo = ws.IsGitRepo
			existing.IsBare = ws.IsBare
			s.db.Save(&existing)
		} else {
			s.db.Create(ws)
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
			isGitRepo, isBare := s.detectRepoKind(fullPath)

			workspaces = append(workspaces, &Workspace{
				Name:      entry.Name(),
				Path:      fullPath,
				IsGitRepo: isGitRepo,
				IsBare:    isBare,
			})
		}
	}

	for _, wsPath := range cfg.Workspaces {
		expanded := expandHome(wsPath)
		info, err := s.fs.Stat(expanded)
		if err != nil || !info.IsDir() {
			continue
		}
		isGitRepo, isBare := s.detectRepoKind(expanded)
		workspaces = append(workspaces, &Workspace{
			Name:      filepath.Base(expanded),
			Path:      expanded,
			IsGitRepo: isGitRepo,
			IsBare:    isBare,
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

func (s *WorkspaceStore) detectRepoKind(path string) (isGit, isBare bool) {
	info, err := s.fs.Stat(filepath.Join(path, ".git"))
	if err != nil {
		return false, false
	}
	if info.IsDir() {
		return true, false
	}
	bareInfo, err := s.fs.Stat(filepath.Join(path, ".bare"))
	if err == nil && bareInfo.IsDir() {
		return true, true
	}
	return false, false
}

func (s *WorkspaceStore) isGitRepository(path string) bool {
	isGit, _ := s.detectRepoKind(path)
	return isGit
}

func (s *WorkspaceStore) ConfiguredRoots() []string {
	cfg, err := s.loadConfig()
	if err != nil || cfg == nil {
		return nil
	}
	roots := make([]string, 0, len(cfg.WorkspaceRoots))
	for _, r := range cfg.WorkspaceRoots {
		roots = append(roots, expandHome(r))
	}
	return roots
}
