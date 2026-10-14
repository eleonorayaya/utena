package mux

import (
	"context"
	"fmt"
	"sync"
)

type FakeDriver struct {
	mu            sync.Mutex
	sessions      map[string]bool
	CreateErr     error
	OnNewSession  func()
	OnKillSession func()
	windows       map[string][]Window
	onEvent       func(Event)
}

var _ Driver = (*FakeDriver)(nil)

func NewFakeDriver() *FakeDriver {
	return &FakeDriver{sessions: make(map[string]bool), windows: make(map[string][]Window)}
}

func (m *FakeDriver) NewWindow(sessionName, startDir, command string, env map[string]string) error {
	return nil
}

func (m *FakeDriver) NewSession(name, startDir string, env map[string]string) error {
	if m.OnNewSession != nil {
		m.OnNewSession()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CreateErr != nil {
		return m.CreateErr
	}
	if m.sessions[name] {
		return fmt.Errorf("duplicate session: %s", name)
	}
	m.sessions[name] = true
	return nil
}

func (m *FakeDriver) KillSession(name string) error {
	if m.OnKillSession != nil {
		m.OnKillSession()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
	return nil
}

func (m *FakeDriver) HasSession(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[name]
}

func (m *FakeDriver) ListSessions() ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.sessions))
	for name := range m.sessions {
		names = append(names, name)
	}
	return names, nil
}

func (m *FakeDriver) ListWindows(sessionName string) ([]Window, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.windows[sessionName], nil
}

func (m *FakeDriver) Subscribe(ctx context.Context, onEvent func(Event)) error {
	m.mu.Lock()
	m.onEvent = onEvent
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	m.onEvent = nil
	m.mu.Unlock()
	return ctx.Err()
}

func (m *FakeDriver) Emit(ev Event) bool {
	m.mu.Lock()
	onEvent := m.onEvent
	m.mu.Unlock()
	if onEvent == nil {
		return false
	}
	onEvent(ev)
	return true
}

func (m *FakeDriver) SetWindows(sessionName string, windows []Window) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.windows[sessionName] = windows
}

func (m *FakeDriver) SetCreateErr(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateErr = err
}

func (m *FakeDriver) RemoveSession(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, name)
}
