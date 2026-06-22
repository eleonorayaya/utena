package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
)

// BareOpTimeout bounds a background bare operation (clone or migrate-to-bare).
const BareOpTimeout = 45 * time.Minute

type CloneFromURLRequest struct {
	CloneURL string
	RootPath string
	DirName  string
}

type WorkspaceService struct {
	store      *WorkspaceStore
	gitService *git.GitService
	progress   *progressTracker
}

func NewWorkspaceService(store *WorkspaceStore, gitService *git.GitService) *WorkspaceService {
	return &WorkspaceService{
		store:      store,
		gitService: gitService,
		progress:   newProgressTracker(),
	}
}

// OnAppStart fails any workspace left mid-operation by a previous daemon run:
// its background goroutine is gone, so the clone/migration will never complete.
func (s *WorkspaceService) OnAppStart(ctx context.Context) error {
	for _, ws := range s.store.List() {
		if ws.IsBusy() {
			ws.Status = StatusFailed
			ws.StatusError = "interrupted by daemon restart"
			if err := s.store.Update(&ws); err != nil {
				slog.Warn("failed to fail interrupted workspace on startup", "workspace", ws.Name, "error", err)
			}
		}
	}
	return nil
}

func (s *WorkspaceService) OnAppEnd(ctx context.Context) error {

	return nil
}

func (s *WorkspaceService) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	list := s.store.List()
	for i := range list {
		s.overlayStatus(&list[i])
	}
	return list, nil
}

func (s *WorkspaceService) GetWorkspace(ctx context.Context, id uint) (*Workspace, error) {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	s.overlayStatus(ws)
	return ws, nil
}

// overlayStatus attaches the live (unpersisted) progress line for in-flight
// operations. Status itself is normalized at load time by Workspace.AfterFind.
func (s *WorkspaceService) overlayStatus(ws *Workspace) {
	if ws.IsBusy() {
		ws.Progress = s.progress.get(ws.ID)
	}
}

func (s *WorkspaceService) GetWorkspaceByPath(ctx context.Context, path string) (*Workspace, error) {
	return s.store.GetByPath(path)
}

func (s *WorkspaceService) GetWorkspaceByRepoID(ctx context.Context, repoID uint) (*Workspace, error) {
	return s.store.GetByRepoID(repoID)
}

func (s *WorkspaceService) Touch(ctx context.Context, id uint) error {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	ws.LastUsedAt = time.Now()
	return s.store.Update(ws)
}

func (s *WorkspaceService) SetWorkspaceHidden(ctx context.Context, id uint, hidden bool) error {
	return s.store.SetHidden(id, hidden)
}

func (s *WorkspaceService) DeleteWorkspace(ctx context.Context, id uint) error {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	if err := s.store.Delete(id); err != nil {
		return err
	}

	s.store.RemoveWorkspaceFromConfig(ws.Path)
	return nil
}

func (s *WorkspaceService) AddWorkspace(ctx context.Context, path string, asRoot bool) (*Workspace, error) {
	if asRoot {
		added, err := s.store.AddWorkspaceRoot(path)
		if err != nil {
			return nil, err
		}
		s.resolveRepoIDs(ctx, added)
		return nil, nil
	}

	ws, err := s.store.AddWorkspace(path)
	if err != nil {
		return nil, err
	}
	if !ws.IsGitRepo {
		if delErr := s.store.Delete(ws.ID); delErr != nil {
			slog.Warn("failed to roll back non-git workspace", "path", path, "error", delErr)
		}
		s.store.RemoveWorkspaceFromConfig(path)
		return nil, common.NewInvalidRequest(fmt.Sprintf("path %q is not a git repository — add the parent directory as root to scan for git repos within", path))
	}

	if s.gitService != nil && ws.RepoID == nil {
		repo, err := s.gitService.FindOrCreateRepo(ctx, ws.Path)
		if err != nil {
			slog.Warn("failed to resolve repo id for workspace", "workspace", ws.Name, "error", err)
			return ws, nil
		}
		if existing, lookupErr := s.store.GetByRepoID(repo.ID); lookupErr == nil && existing.ID != ws.ID {
			if delErr := s.store.Delete(ws.ID); delErr != nil {
				slog.Warn("failed to roll back duplicate-repo workspace", "path", path, "error", delErr)
			}
			s.store.RemoveWorkspaceFromConfig(path)
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %q already tracks this repository — remove it first or use a different repo", existing.Name))
		}
		ws.RepoID = &repo.ID
		if updateErr := s.store.Update(ws); updateErr != nil {
			slog.Warn("failed to persist repo id for workspace", "workspace", ws.Name, "error", updateErr)
		}
	}
	return ws, nil
}

func (s *WorkspaceService) resolveCloneTarget(req CloneFromURLRequest) (cloneURL, targetPath string, err error) {
	cloneURL = strings.TrimSpace(req.CloneURL)
	if cloneURL == "" {
		return "", "", common.NewInvalidRequest("clone_url is required")
	}
	if s.gitService == nil {
		return "", "", fmt.Errorf("git service not configured")
	}

	roots := s.store.ConfiguredRoots()
	rootPath, err := resolveRootPath(req.RootPath, roots)
	if err != nil {
		return "", "", err
	}

	dirName := strings.TrimSpace(req.DirName)
	if dirName == "" {
		_, repoName, parseErr := s.gitService.ParseRepoFullName(cloneURL)
		if parseErr != nil {
			return "", "", common.WrapInvalidRequest("cannot derive directory name from clone URL", parseErr)
		}
		dirName = repoName
	}
	if strings.ContainsAny(dirName, "/\\") {
		return "", "", common.NewInvalidRequest(fmt.Sprintf("invalid directory name %q (must not contain path separators)", dirName))
	}

	targetPath = filepath.Join(rootPath, dirName)
	if _, statErr := os.Stat(targetPath); statErr == nil {
		return "", "", common.NewConflict(fmt.Sprintf("target path already exists: %s", targetPath))
	} else if !os.IsNotExist(statErr) {
		return "", "", fmt.Errorf("failed to inspect target path %s: %w", targetPath, statErr)
	}

	return cloneURL, targetPath, nil
}

// StartClone validates synchronously (returning user errors immediately),
// creates the workspace row up-front in the "cloning" state, and runs the slow
// clone in the background. Callers observe progress and completion via the
// workspace's Status/Progress, not a separate job handle.
func (s *WorkspaceService) StartClone(ctx context.Context, req CloneFromURLRequest) (*Workspace, error) {
	cloneURL, targetPath, err := s.resolveCloneTarget(req)
	if err != nil {
		return nil, err
	}

	ws := &Workspace{
		Name:   filepath.Base(targetPath),
		Path:   targetPath,
		Status: StatusCloning,
	}
	if err := s.store.Add(ws); err != nil {
		return nil, err
	}

	id := ws.ID
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), BareOpTimeout)
		defer cancel()
		if cloneErr := s.gitService.CloneBareWorkspace(bgCtx, cloneURL, targetPath, s.ProgressFn(id)); cloneErr != nil {
			_ = os.RemoveAll(targetPath)
			s.FailBareOperation(id, common.WrapInvalidRequest("git clone failed", cloneErr))
			return
		}
		if err := s.FinalizeBareWorkspace(bgCtx, id); err != nil {
			s.FailBareOperation(id, err)
		}
	}()
	return ws, nil
}

// ProgressFn returns the progress callback for a background bare operation; each
// line is recorded against the workspace for status reads to surface.
func (s *WorkspaceService) ProgressFn(id uint) func(string) {
	return func(line string) { s.progress.set(id, line) }
}

// BeginMigration marks a workspace as migrating and returns the updated row.
func (s *WorkspaceService) BeginMigration(ctx context.Context, id uint) (*Workspace, error) {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	ws.Status = StatusMigrating
	ws.StatusError = ""
	if err := s.store.Update(ws); err != nil {
		return nil, err
	}
	s.overlayStatus(ws)
	return ws, nil
}

// FinalizeBareWorkspace clears live progress and promotes a freshly-cloned or
// migrated workspace to ready: detect the on-disk git/bare kind, resolve the
// repo, and record the path in config. Shared terminal step for both clone and
// migrate-to-bare.
func (s *WorkspaceService) FinalizeBareWorkspace(ctx context.Context, id uint) error {
	s.progress.clear(id)
	ws, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	// Reaching here means the bare clone/migrate succeeded, so the workspace is
	// a bare git repo by construction.
	ws.IsGitRepo = true
	ws.IsBare = true
	ws.Status = StatusReady
	ws.StatusError = ""

	if s.gitService != nil && ws.RepoID == nil {
		if repo, repoErr := s.gitService.FindOrCreateRepo(ctx, ws.Path); repoErr == nil {
			ws.RepoID = &repo.ID
		} else {
			slog.Warn("failed to resolve repo id for workspace", "workspace", ws.Name, "error", repoErr)
		}
	}

	if err := s.store.Update(ws); err != nil {
		return err
	}
	if err := s.store.appendWorkspacePath(ws.Path); err != nil {
		slog.Warn("failed to record workspace path in config", "workspace", ws.Name, "error", err)
	}
	return nil
}

// FailBareOperation clears live progress and marks the workspace failed. Shared
// error terminal for both clone and migrate-to-bare.
func (s *WorkspaceService) FailBareOperation(id uint, cause error) {
	s.progress.clear(id)
	ws, err := s.store.GetByID(id)
	if err != nil {
		slog.Warn("failed to load workspace to mark failed", "id", id, "error", err)
		return
	}
	ws.Status = StatusFailed
	ws.StatusError = cause.Error()
	if err := s.store.Update(ws); err != nil {
		slog.Warn("failed to mark workspace failed", "workspace", ws.Name, "error", err)
	}
}

func resolveRootPath(requested string, configured []string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		expanded := expandHome(requested)
		for _, root := range configured {
			if root == expanded {
				return root, nil
			}
		}
		return "", common.NewInvalidRequest(fmt.Sprintf("root_path %q is not a configured workspace root", requested))
	}
	if len(configured) == 0 {
		return "", common.NewConflict("no workspace roots configured — add one before cloning from URL")
	}
	if len(configured) > 1 {
		return "", common.NewConflict("multiple workspace roots configured — specify root_path")
	}
	return configured[0], nil
}

func (s *WorkspaceService) resolveRepoIDs(ctx context.Context, workspaces []Workspace) {
	if s.gitService == nil {
		return
	}
	for i := range workspaces {
		ws := &workspaces[i]
		if !ws.IsGitRepo || ws.RepoID != nil {
			continue
		}
		repo, err := s.gitService.FindOrCreateRepo(ctx, ws.Path)
		if err != nil {
			slog.Warn("failed to resolve repo id for workspace", "workspace", ws.Name, "error", err)
			continue
		}
		ws.RepoID = &repo.ID
		if err := s.store.Update(ws); err != nil {
			slog.Warn("failed to persist repo id for workspace", "workspace", ws.Name, "error", err)
		}
	}
}
