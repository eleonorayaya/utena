package wm

import (
	"context"
	"fmt"
	"sync"
)

type Move struct {
	WindowID  string
	Workspace string
}

type FakeDriver struct {
	mu       sync.Mutex
	windows  []Window
	moves    []Move
	focused  []string
	MoveErr  error
	ListErr  error
	FocusErr error
	OnFocus  func(windowID string)
}

var _ Driver = (*FakeDriver)(nil)

func NewFakeDriver() *FakeDriver {
	return &FakeDriver{}
}

func (f *FakeDriver) Add(id, title, workspace string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.windows = append(f.windows, Window{ID: id, Title: title, Workspace: workspace})
}

func (f *FakeDriver) SetTitle(id, title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.indexLocked(id); i >= 0 {
		f.windows[i].Title = title
	}
}

func (f *FakeDriver) Remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.indexLocked(id); i >= 0 {
		f.windows = append(f.windows[:i], f.windows[i+1:]...)
	}
}

func (f *FakeDriver) Workspace(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i := f.indexLocked(id); i >= 0 {
		return f.windows[i].Workspace
	}
	return ""
}

func (f *FakeDriver) Moves() []Move {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Move(nil), f.moves...)
}

func (f *FakeDriver) ListWindows(ctx context.Context, bundleID string) ([]Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	return append([]Window(nil), f.windows...), nil
}

func (f *FakeDriver) MoveToWorkspace(ctx context.Context, windowID, workspace string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.MoveErr != nil {
		return f.MoveErr
	}
	i := f.indexLocked(windowID)
	if i < 0 {
		return fmt.Errorf("no window %s", windowID)
	}
	f.windows[i].Workspace = workspace
	f.moves = append(f.moves, Move{WindowID: windowID, Workspace: workspace})
	return nil
}

func (f *FakeDriver) Focused() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.focused...)
}

func (f *FakeDriver) FocusWindow(ctx context.Context, windowID string) error {
	f.mu.Lock()
	err := f.FocusErr
	if err == nil {
		f.focused = append(f.focused, windowID)
	}
	onFocus := f.OnFocus
	f.mu.Unlock()
	if onFocus != nil {
		onFocus(windowID)
	}
	return err
}

func (f *FakeDriver) indexLocked(id string) int {
	for i, w := range f.windows {
		if w.ID == id {
			return i
		}
	}
	return -1
}
