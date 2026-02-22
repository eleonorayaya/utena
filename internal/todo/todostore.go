package todo

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync"

	"github.com/spf13/afero"
)

type TodoStore struct {
	mu        sync.RWMutex
	todos     map[string]*Todo
	fs        afero.Fs
	configDir string
}

func NewTodoStore(fs afero.Fs, configDir string) *TodoStore {
	return &TodoStore{
		todos:     make(map[string]*Todo),
		fs:        fs,
		configDir: configDir,
	}
}

func (s *TodoStore) GetByID(id string) (*Todo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.todos[id]
	if !ok {
		return nil, ErrTodoNotFound
	}

	copy := *t
	return &copy, nil
}

func (s *TodoStore) List() []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	todos := make([]Todo, 0, len(s.todos))
	for _, t := range s.todos {
		todos = append(todos, *t)
	}

	sort.Slice(todos, func(i, j int) bool {
		return todos[i].CreatedAt.After(todos[j].CreatedAt)
	})

	return todos
}

func (s *TodoStore) ListByWorkspaceID(workspaceID string) []Todo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	todos := make([]Todo, 0)
	for _, t := range s.todos {
		if t.WorkspaceID == workspaceID {
			todos = append(todos, *t)
		}
	}

	sort.Slice(todos, func(i, j int) bool {
		return todos[i].CreatedAt.After(todos[j].CreatedAt)
	})

	return todos
}

func (s *TodoStore) Add(t *Todo) error {
	if t == nil {
		return errors.New("todo cannot be nil")
	}

	if t.ID == "" {
		return errors.New("todo ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	copy := *t
	s.todos[t.ID] = &copy
	s.save()
	return nil
}

func (s *TodoStore) Delete(id string) error {
	if id == "" {
		return errors.New("todo ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.todos[id]; !exists {
		return ErrTodoNotFound
	}

	delete(s.todos, id)
	s.save()
	return nil
}

func (s *TodoStore) todosPath() string {
	return filepath.Join(s.configDir, "todos.json")
}

func (s *TodoStore) save() {
	s.fs.MkdirAll(s.configDir, 0755)

	data, err := json.Marshal(s.todos)
	if err != nil {
		return
	}

	afero.WriteFile(s.fs, s.todosPath(), data, 0644)
}

func (s *TodoStore) OnAppStart(ctx context.Context) error {
	data, err := afero.ReadFile(s.fs, s.todosPath())
	if err != nil {
		return nil
	}

	loaded := make(map[string]*Todo)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.todos = loaded

	return nil
}

func (s *TodoStore) OnAppEnd(ctx context.Context) error {
	return nil
}
