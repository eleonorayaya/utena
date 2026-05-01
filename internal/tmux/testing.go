package tmux

import (
	"fmt"
	"sync"
)

type SpawnedWindow struct {
	SessionName string
	StartDir    string
	Command     string
}

type MockRunner struct {
	mu             sync.Mutex
	Sessions       map[string]bool
	SpawnedWindows []SpawnedWindow
	CreateErr      error
	KillErr        error
}

func NewMockRunner() *MockRunner {
	return &MockRunner{Sessions: make(map[string]bool)}
}

func (m *MockRunner) newWindow(sessionName, startDir, command string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SpawnedWindows = append(m.SpawnedWindows, SpawnedWindow{
		SessionName: sessionName,
		StartDir:    startDir,
		Command:     command,
	})
	return nil
}

func (m *MockRunner) newSession(name, startDir string, env map[string]string) error {
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

func (m *MockRunner) switchClient(targetSession string) error {
	return nil
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

func (m *MockRunner) command(args ...string) (string, error) {
	return "", nil
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
