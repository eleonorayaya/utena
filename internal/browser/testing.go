package browser

import (
	"context"
	"fmt"
	"sync"
)

type FakeDriver struct {
	mu      sync.Mutex
	windows []Window
	nextID  int
	focused []string
	events  chan Event
	stopped chan struct{}
	ListErr error
}

var _ Driver = (*FakeDriver)(nil)

func NewFakeDriver() *FakeDriver {
	return &FakeDriver{}
}

func (f *FakeDriver) id(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s%d", prefix, f.nextID)
}

func (f *FakeDriver) AddWindow(urls ...string) Window {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.addWindowLocked(urls)
	w.Tabs = append([]Tab(nil), w.Tabs...)
	return w
}

func (f *FakeDriver) addWindowLocked(urls []string) Window {
	w := Window{ID: f.id("w")}
	for i, u := range urls {
		w.Tabs = append(w.Tabs, Tab{ID: f.id("t"), URL: u, Active: i == len(urls)-1})
	}
	f.windows = append(f.windows, w)
	return w
}

func (f *FakeDriver) RemoveWindow(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, w := range f.windows {
		if w.ID == id {
			f.windows = append(f.windows[:i], f.windows[i+1:]...)
			return
		}
	}
}

func (f *FakeDriver) Window(id string) (Window, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.windows {
		if w.ID == id {
			w.Tabs = append([]Tab(nil), w.Tabs...)
			return w, true
		}
	}
	return Window{}, false
}

func (f *FakeDriver) SetFocused(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		f.windows[i].Focused = f.windows[i].ID == id
	}
}

func (f *FakeDriver) Focused() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.focused...)
}

func (f *FakeDriver) ListWindows(ctx context.Context) ([]Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	out := make([]Window, len(f.windows))
	for i, w := range f.windows {
		w.Tabs = append([]Tab(nil), w.Tabs...)
		out[i] = w
	}
	return out, nil
}

func (f *FakeDriver) CreateWindow(ctx context.Context, urls []string, focused bool) (Window, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w := f.addWindowLocked(urls)
	if focused {
		f.focused = append(f.focused, w.ID)
	}
	w.Tabs = append([]Tab(nil), w.Tabs...)
	return w, nil
}

func (f *FakeDriver) OpenTab(ctx context.Context, windowID, url string) (Tab, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		if f.windows[i].ID != windowID {
			continue
		}
		for j := range f.windows[i].Tabs {
			f.windows[i].Tabs[j].Active = false
		}
		t := Tab{ID: f.id("t"), URL: url, Active: true}
		f.windows[i].Tabs = append(f.windows[i].Tabs, t)
		return t, nil
	}
	return Tab{}, fmt.Errorf("no window %s", windowID)
}

func (f *FakeDriver) ActivateTab(ctx context.Context, windowID, tabID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		if f.windows[i].ID != windowID {
			continue
		}
		for j := range f.windows[i].Tabs {
			f.windows[i].Tabs[j].Active = f.windows[i].Tabs[j].ID == tabID
		}
		return nil
	}
	return fmt.Errorf("no window %s", windowID)
}

func (f *FakeDriver) FocusWindow(ctx context.Context, windowID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.focused = append(f.focused, windowID)
	return nil
}

func (f *FakeDriver) Subscribe(ctx context.Context, onEvent func(Event)) error {
	events := make(chan Event)
	stopped := make(chan struct{})
	f.mu.Lock()
	f.events = events
	f.stopped = stopped
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		if f.events == events {
			f.events = nil
			f.stopped = nil
		}
		f.mu.Unlock()
		close(stopped)
	}()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev := <-events:
			onEvent(ev)
		}
	}
}

func (f *FakeDriver) Emit(ev Event) bool {
	f.mu.Lock()
	events, stopped := f.events, f.stopped
	f.mu.Unlock()
	if events == nil {
		return false
	}
	select {
	case events <- ev:
		return true
	case <-stopped:
		return false
	}
}

type FakeSessions struct {
	mu            sync.Mutex
	Attached      uint
	Activated     []uint
	ActivateErr   error
	IsAttachedErr error
}

var _ Sessions = (*FakeSessions)(nil)

func (f *FakeSessions) IsAttached(ctx context.Context, sessionID uint) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.IsAttachedErr != nil {
		return false, f.IsAttachedErr
	}
	return f.Attached == sessionID, nil
}

func (f *FakeSessions) Activate(ctx context.Context, sessionID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ActivateErr != nil {
		return f.ActivateErr
	}
	f.Activated = append(f.Activated, sessionID)
	f.Attached = sessionID
	return nil
}
