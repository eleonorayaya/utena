package todo

import (
	"context"

	"github.com/eleonorayaya/utena/internal/workspace"
)

type TodoService struct {
	store            *TodoStore
	workspaceService *workspace.WorkspaceService
}

func NewTodoService(store *TodoStore, workspaceService *workspace.WorkspaceService) *TodoService {
	return &TodoService{
		store:            store,
		workspaceService: workspaceService,
	}
}

func (s *TodoService) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *TodoService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *TodoService) Create(ctx context.Context, name, description string, workspaceID *uint) (*Todo, error) {
	if workspaceID != nil && *workspaceID != 0 {
		_, err := s.workspaceService.GetWorkspace(ctx, *workspaceID)
		if err != nil {
			return nil, err
		}
	}

	t := &Todo{
		Name:        name,
		Description: description,
		WorkspaceID: workspaceID,
	}

	if err := s.store.Add(t); err != nil {
		return nil, err
	}

	t, _ = s.store.GetByID(t.ID)
	return t, nil
}

func (s *TodoService) List(ctx context.Context) ([]Todo, error) {
	return s.store.List(), nil
}

func (s *TodoService) ListByWorkspace(ctx context.Context, workspaceID uint) ([]Todo, error) {
	return s.store.ListByWorkspaceID(workspaceID), nil
}

func (s *TodoService) Delete(ctx context.Context, id uint) error {
	return s.store.Delete(id)
}
