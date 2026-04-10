package workspace

import (
	"context"
	"time"
)

type WorkspaceService struct {
	store *WorkspaceStore
}

func NewWorkspaceService(store *WorkspaceStore) *WorkspaceService {
	return &WorkspaceService{
		store: store,
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

func (s *WorkspaceService) AddWorkspace(ctx context.Context, path string, asRoot bool) (*Workspace, error) {
	if asRoot {
		_, err := s.store.AddWorkspaceRoot(path)
		return nil, err
	}
	return s.store.AddWorkspace(path)
}
