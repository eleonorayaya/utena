package todo

import (
	"context"
	"errors"
	"strings"

	"github.com/eleonorayaya/utena/internal/db"
	"gorm.io/gorm"
)

type TodoStore struct {
	db db.Database
}

func NewTodoStore(database db.Database) *TodoStore {
	return &TodoStore{
		db: database,
	}
}

func (s *TodoStore) GetByID(id uint) (*Todo, error) {
	var t Todo
	if err := s.db.Joins("Workspace").First(&t, "todos.id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTodoNotFound
		}
		return nil, err
	}
	s.populateWorkspaceName(&t)
	return &t, nil
}

func (s *TodoStore) List() []Todo {
	var todos []Todo
	s.db.Joins("Workspace").Order("todos.created_at DESC").Find(&todos)
	for i := range todos {
		s.populateWorkspaceName(&todos[i])
	}
	return todos
}

func (s *TodoStore) ListByWorkspaceID(workspaceID uint) []Todo {
	var todos []Todo
	s.db.Joins("Workspace").Where("todos.workspace_id = ?", workspaceID).Order("todos.created_at DESC").Find(&todos)
	for i := range todos {
		s.populateWorkspaceName(&todos[i])
	}
	return todos
}

func (s *TodoStore) Add(t *Todo) error {
	if t == nil {
		return errors.New("todo cannot be nil")
	}

	omitFields := []string{"Workspace"}

	if err := s.db.Omit(omitFields...).Create(t).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return errors.New("todo with this ID already exists")
		}
		return err
	}
	return nil
}

func (s *TodoStore) Delete(id uint) error {
	if id == 0 {
		return errors.New("todo ID cannot be zero")
	}

	var existing Todo
	if err := s.db.First(&existing, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTodoNotFound
		}
		return err
	}

	return s.db.Delete(&Todo{}, "id = ?", id).Error
}

func (s *TodoStore) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *TodoStore) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *TodoStore) populateWorkspaceName(t *Todo) {
	if t.Workspace != nil {
		t.WorkspaceName = t.Workspace.Name
	}
}
