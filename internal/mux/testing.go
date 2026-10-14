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
	paneShells    map[int]string
	onEvent       func(Event)
}

var _ Driver = (*FakeDriver)(nil)

func NewFakeDriver() *FakeDriver {
	return &FakeDriver{sessions: make(map[string]bool), windows: make(map[string][]Window), paneShells: make(map[int]string)}
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

func (m *FakeDriver) PaneShells(procs []Process) (map[int]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[int]string)
	for _, p := range procs {
		if name, ok := m.paneShells[p.PID]; ok {
			out[p.PID] = name
		}
	}
	return out, nil
}

func (m *FakeDriver) SetPaneShell(pid int, sessionName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paneShells[pid] = sessionName
}

type FakeProcessLister struct {
	mu    sync.Mutex
	procs []Process
	Err   error
}

func (f *FakeProcessLister) ListProcesses(ctx context.Context) ([]Process, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Process(nil), f.procs...), f.Err
}

func (f *FakeProcessLister) SetProcesses(procs []Process) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.procs = procs
}
