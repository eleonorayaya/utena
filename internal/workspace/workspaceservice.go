package workspace

import (
	"context"
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

func (s *WorkspaceService) GetWorkspace(ctx context.Context, id string) (*Workspace, error) {
	return s.store.GetByID(id)
}

func (s *WorkspaceService) GetWorkspaceByPath(ctx context.Context, path string) (*Workspace, error) {
	return s.store.GetByPath(path)
}

func (s *WorkspaceService) AddWorkspace(ctx context.Context, path string, asRoot bool) (*Workspace, error) {
	if asRoot {
		_, err := s.store.AddWorkspaceRoot(path)
		return nil, err
	}
	return s.store.AddWorkspace(path)
}
