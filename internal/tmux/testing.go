package tmux

import (
	"context"
	"fmt"
	"sync"
)

type SpawnedWindow struct {
	SessionName string
	StartDir    string
	Command     string
	Env         map[string]string
}

type MockRunner struct {
	mu             sync.Mutex
	Sessions       map[string]bool
	SpawnedWindows []SpawnedWindow
	CreateErr      error
	KillErr        error
	OnNewSession   func()
	OnKillSession  func()
}

func NewMockRunner() *MockRunner {
	return &MockRunner{Sessions: make(map[string]bool)}
}

func (m *MockRunner) newWindow(sessionName, startDir, command string, env map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SpawnedWindows = append(m.SpawnedWindows, SpawnedWindow{
		SessionName: sessionName,
		StartDir:    startDir,
		Command:     command,
		Env:         env,
	})
	return nil
}

func (m *MockRunner) newSession(name, startDir string, env map[string]string) error {
	if m.OnNewSession != nil {
		m.OnNewSession()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return m.CreateErr
	}
	if m.Sessions[name] {
		return fmt.Errorf("duplicate session: %s", name)
	}
	m.Sessions[name] = true
	return nil
}

func (m *MockRunner) killSession(name string) error {
	if m.OnKillSession != nil {
		m.OnKillSession()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.KillErr != nil {
		return m.KillErr
	}
	delete(m.Sessions, name)
	return nil
}

func (m *MockRunner) hasSession(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Sessions[name]
}

func (m *MockRunner) listSessionNames() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.Sessions))
	for name := range m.Sessions {
		names = append(names, name)
	}
	return names, nil
}

func (m *MockRunner) listWindows(sessionName string) ([]Window, error) {
	return nil, nil
}

func (m *MockRunner) subscribe(ctx context.Context, types []string, onEvent func(tuiosEvent)) error {
	<-ctx.Done()
	return ctx.Err()
}

func (m *MockRunner) SetCreateErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateErr = err
}

func (m *MockRunner) SetKillErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KillErr = err
}

func (m *MockRunner) HasSessionByName(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Sessions[name]
}

func (m *MockRunner) RemoveSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Sessions, name)
}
