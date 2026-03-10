package todo

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

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

func (s *TodoService) Create(ctx context.Context, name, description, workspaceID string) (*Todo, error) {
	if workspaceID != "" {
		_, err := s.workspaceService.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
	}

	id := generateID(name, time.Now())

	t := &Todo{
		ID:          id,
		Name:        name,
		Description: description,
		WorkspaceID: workspaceID,
		CreatedAt:   time.Now(),
	}

	if err := s.store.Add(t); err != nil {
		return nil, err
	}

	t, _ = s.store.GetByID(id)
	return t, nil
}

func (s *TodoService) List(ctx context.Context) ([]Todo, error) {
	return s.store.List(), nil
}

func (s *TodoService) ListByWorkspace(ctx context.Context, workspaceID string) ([]Todo, error) {
	return s.store.ListByWorkspaceID(workspaceID), nil
}

func (s *TodoService) Delete(ctx context.Context, id string) error {
	return s.store.Delete(id)
}

func generateID(name string, t time.Time) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s-%d", name, t.UnixNano())))
	return fmt.Sprintf("%x", h[:8])
}
