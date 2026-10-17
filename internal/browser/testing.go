package browser

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

type FakeDriver struct {
	mu        sync.Mutex
	windows   []Window
	nextID    int
	focused   []string
	closed    []string
	events    chan Event
	stopped   chan struct{}
	ListErr   error
	CreateErr func(urls []string) error
	CloseErr  error
	OnCreate  func(Window)
	OnFocus   func(windowID string)
	OnClose   func(windowID string)
	outdated  atomic.Bool
	Caps      Capabilities
}

var (
	_ Driver             = (*FakeDriver)(nil)
	_ StatusReporter     = (*FakeDriver)(nil)
	_ CapabilityReporter = (*FakeDriver)(nil)
)

func (f *FakeDriver) Capabilities() Capabilities {
	return f.Caps
}

func (f *FakeDriver) SetOutdated(outdated bool) {
	f.outdated.Store(outdated)
}

func (f *FakeDriver) Outdated() bool {
	return f.outdated.Load()
}

func (f *FakeDriver) UpdateHint() string {
	return "reload the fake extension"
}

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

func (f *FakeDriver) SetTitle(windowID, title string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.windows {
		if f.windows[i].ID != windowID {
			continue
		}
		for j := range f.windows[i].Tabs {
			if f.windows[i].Tabs[j].Active {
				f.windows[i].Tabs[j].Title = title
			}
		}
	}
}

func (f *FakeDriver) AppBundleID() string {
	return "fake.browser"
}

func (f *FakeDriver) WindowTitle(w Window) string {
	t, ok := w.ActiveTab()
	if !ok || t.Title == "" {
		return ""
	}
	return t.Title + " - Fake"
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
	if f.CreateErr != nil {
		if err := f.CreateErr(urls); err != nil {
			f.mu.Unlock()
			return Window{}, err
		}
	}
	w := f.addWindowLocked(urls)
	if focused {
		f.focused = append(f.focused, w.ID)
	}
	w.Tabs = append([]Tab(nil), w.Tabs...)
	onCreate := f.OnCreate
	f.mu.Unlock()
	if onCreate != nil {
		onCreate(w)
	}
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
	f.focused = append(f.focused, windowID)
	onFocus := f.OnFocus
	f.mu.Unlock()
	if onFocus != nil {
		onFocus(windowID)
	}
	return nil
}

func (f *FakeDriver) Closed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.closed...)
}

func (f *FakeDriver) CloseWindow(ctx context.Context, windowID string) error {
	f.mu.Lock()
	if f.CloseErr != nil {
		f.mu.Unlock()
		return f.CloseErr
	}
	idx := -1
	for i, w := range f.windows {
		if w.ID == windowID {
			idx = i
			break
		}
	}
	if idx < 0 {
		f.mu.Unlock()
		return fmt.Errorf("no window %s", windowID)
	}
	f.windows = append(f.windows[:idx], f.windows[idx+1:]...)
	f.closed = append(f.closed, windowID)
	onClose := f.OnClose
	f.mu.Unlock()
	if onClose != nil {
		onClose(windowID)
	}
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
	AttachedID    uint
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
	return f.AttachedID == sessionID, nil
}

func (f *FakeSessions) Activate(ctx context.Context, sessionID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ActivateErr != nil {
		return f.ActivateErr
	}
	f.Activated = append(f.Activated, sessionID)
	f.AttachedID = sessionID
	return nil
}

func (f *FakeSessions) Attached(ctx context.Context) (uint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.AttachedID, nil
}
