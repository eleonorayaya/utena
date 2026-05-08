package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
)

type WorkspaceService struct {
	store      *WorkspaceStore
	gitService *git.GitService
}

func NewWorkspaceService(store *WorkspaceStore, gitService *git.GitService) *WorkspaceService {
	return &WorkspaceService{
		store:      store,
		gitService: gitService,
	}
}

func (s *WorkspaceService) OnAppStart(ctx context.Context) error {

	return nil
}

func (s *WorkspaceService) OnAppEnd(ctx context.Context) error {

	return nil
}

func (s *WorkspaceService) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	return s.store.List(), nil
}

func (s *WorkspaceService) GetWorkspace(ctx context.Context, id uint) (*Workspace, error) {
	return s.store.GetByID(id)
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

func (s *WorkspaceService) MarkAsBare(ctx context.Context, id uint) error {
	ws, err := s.store.GetByID(id)
	if err != nil {
		return err
	}
	ws.IsBare = true
	return s.store.Update(ws)
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
		if repo, err := s.gitService.FindOrCreateRepo(ctx, ws.Path); err == nil {
			ws.RepoID = &repo.ID
			if updateErr := s.store.Update(ws); updateErr != nil {
				slog.Warn("failed to persist repo id for workspace", "workspace", ws.Name, "error", updateErr)
			}
		} else {
			slog.Warn("failed to resolve repo id for workspace", "workspace", ws.Name, "error", err)
		}
	}
	return ws, nil
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
