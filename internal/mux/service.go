package mux

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
)

type Service struct {
	driver            Driver
	store             *Store
	eventBus          eventbus.EventBus
	windowsBySession  map[string][]Window
	windowsMu         sync.RWMutex
	nameLocks         sync.Map
	activationsMu     sync.Mutex
	activationWaiters []chan Activation
	resources         *resourceSampler
}

func (t *Service) lockName(name string) func() {
	actual, _ := t.nameLocks.LoadOrStore(name, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func NewService(driver Driver, store *Store, bus eventbus.EventBus) *Service {
	t := &Service{
		driver:           driver,
		store:            store,
		eventBus:         bus,
		windowsBySession: make(map[string][]Window),
		resources:        newResourceSampler(PSLister{}, driver),
	}
	bus.Subscribe(eventbus.SessionActivated, t.handleSessionActivated)
	return t
}

func (t *Service) OnAppStart(ctx context.Context) error {
	if err := t.store.BackfillStatus(); err != nil {
		slog.WarnContext(ctx, "mux: backfill status from is_alive failed", "error", err)
	}
	go t.followEvents(ctx)
	return nil
}

func (t *Service) followEvents(ctx context.Context) {
	lastErr := ""
	for ctx.Err() == nil {
		err := t.driver.Subscribe(ctx, func(ev Event) {
			lastErr = ""
			t.handleEvent(ctx, ev)
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil && err.Error() != lastErr {
			lastErr = err.Error()
			slog.Warn("mux event stream unavailable; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *Service) handleEvent(ctx context.Context, ev Event) {
	var err error
	switch ev.Type {
	case EventResync:
		t.resync()
	case EventSessionCreated:
		err = t.HandleSessionCreated(ctx, ev.Session)
		t.refreshWindows(ev.Session)
	case EventSessionClosed:
		err = t.HandleSessionClosed(ctx, ev.Session)
	case EventWindowsChanged:
		t.refreshWindows(ev.Session)
	}
	if err != nil {
		slog.Warn("mux event handler failed", "type", ev.Type, "session", ev.Session, "error", err)
	}
}

func (t *Service) resync() {
	names, err := t.driver.ListSessions()
	if err != nil {
		slog.Warn("mux resync: list-sessions failed", "error", err)
		return
	}
	for _, ts := range t.store.List() {
		alive := slices.Contains(names, ts.Name)
		switch {
		case alive && ts.Status == StatusInactive:
			ts.Status = StatusActive
		case !alive && ts.Status == StatusActive:
			ts.Status = StatusInactive
		default:
			continue
		}
		if err := t.store.Update(&ts); err != nil {
			slog.Warn("mux resync: update failed", "mux", ts.Name, "error", err)
		}
	}
	for _, name := range names {
		t.refreshWindows(name)
	}
}

func (t *Service) refreshWindows(sessionName string) {
	if sessionName == "" {
		return
	}
	windows, err := t.driver.ListWindows(sessionName)
	if err != nil {
		slog.Debug("mux list-windows failed", "session", sessionName, "error", err)
		return
	}
	t.windowsMu.Lock()
	defer t.windowsMu.Unlock()
	t.windowsBySession[sessionName] = windows
}

func (t *Service) handleSessionActivated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionActivatedEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}
	t.activationsMu.Lock()
	defer t.activationsMu.Unlock()
	for _, ch := range t.activationWaiters {
		ch <- Activation{SessionName: data.SessionName, Host: data.Host}
	}
	t.activationWaiters = nil
	return nil
}

func (t *Service) WaitForActivation(ctx context.Context) (Activation, error) {
	ch := make(chan Activation, 1)
	t.activationsMu.Lock()
	t.activationWaiters = append(t.activationWaiters, ch)
	t.activationsMu.Unlock()

	select {
	case activation := <-ch:
		return activation, nil
	case <-ctx.Done():
		t.activationsMu.Lock()
		t.activationWaiters = slices.DeleteFunc(t.activationWaiters, func(c chan Activation) bool { return c == ch })
		t.activationsMu.Unlock()
		return Activation{}, ctx.Err()
	}
}

func (t *Service) SessionEnv(name, key string) (string, bool) {
	ts, err := t.store.GetByName(name)
	if err != nil {
		return "", false
	}
	v, ok := ts.Env[key]
	return v, ok
}

func (t *Service) OnAppEnd(ctx context.Context) error {
	return nil
}

func (t *Service) RegisterPending(name, startDir string, env map[string]string) (*Session, error) {
	defer t.lockName(name)()
	if existing, err := t.store.GetByName(name); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrSessionNotFound) {
		return nil, err
	}
	ts := &Session{
		Name:     name,
		StartDir: startDir,
		Env:      env,
		Status:   StatusPending,
	}
	if err := t.store.Add(ts); err != nil {
		if errors.Is(err, ErrSessionAlreadyExists) {
			existing, getErr := t.store.GetByName(name)
			if getErr != nil {
				return nil, getErr
			}
			if existing.Status == StatusActive {
				return nil, ErrSessionAlreadyExists
			}
			return existing, nil
		}
		return nil, err
	}
	return ts, nil
}

func (t *Service) SpawnForRecord(id uint) (*Session, error) {
	ts, err := t.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	defer t.lockName(ts.Name)()
	if !t.driver.HasSession(ts.Name) {
		if err := t.driver.NewSession(ts.Name, ts.StartDir, ts.Env); err != nil {
			return nil, err
		}
	}
	ts.Status = StatusActive
	if err := t.store.Update(ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (t *Service) CreateSession(name, startDir string, env map[string]string) (*Session, error) {
	defer t.lockName(name)()
	if err := t.driver.NewSession(name, startDir, env); err != nil {
		return nil, err
	}
	ts := &Session{
		Name:     name,
		StartDir: startDir,
		Env:      env,
		Status:   StatusActive,
	}
	if err := t.store.Add(ts); err != nil {
		if !errors.Is(err, ErrSessionAlreadyExists) {
			return nil, err
		}
		existing, getErr := t.store.GetByName(name)
		if getErr != nil {
			return nil, getErr
		}
		existing.StartDir = startDir
		existing.Env = env
		existing.Status = StatusActive
		if updateErr := t.store.Update(existing); updateErr != nil {
			return nil, updateErr
		}
		return existing, nil
	}
	return ts, nil
}

func (t *Service) KillSession(id uint) error {
	ts, err := t.store.GetByID(id)
	if err != nil {
		return err
	}
	defer t.lockName(ts.Name)()
	killErr := t.driver.KillSession(ts.Name)
	if killErr != nil {
		slog.Warn("mux driver KillSession failed; marking record inactive anyway", "name", ts.Name, "error", killErr)
	}
	ts.Status = StatusInactive
	return t.store.Update(ts)
}

func (t *Service) HasSession(name string) bool {
	return t.driver.HasSession(name)
}

func (t *Service) GetSession(id uint) (*Session, error) {
	ts, err := t.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	ts.Windows = t.windowsBySession[ts.Name]
	return ts, nil
}

func (t *Service) GetSessionByName(name string) (*Session, error) {
	ts, err := t.store.GetByName(name)
	if err != nil {
		return nil, err
	}
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	ts.Windows = t.windowsBySession[ts.Name]
	return ts, nil
}

func (t *Service) GetOrTrackSession(name, startDir string, env map[string]string) (*Session, error) {
	ts, err := t.store.GetByName(name)
	if err == nil {
		return ts, nil
	}
	if !errors.Is(err, ErrSessionNotFound) {
		return nil, err
	}
	ts = &Session{Name: name, StartDir: startDir, Env: env, Status: StatusActive}
	if err := t.store.Add(ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (t *Service) HandleSessionCreated(ctx context.Context, name string) error {
	t.setStatus(name, StatusActive)
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.MuxSessionCreated,
		Data: eventbus.MuxHookEvent{SessionName: name},
	})
}

func (t *Service) HandleSessionClosed(ctx context.Context, name string) error {
	t.windowsMu.Lock()
	delete(t.windowsBySession, name)
	t.windowsMu.Unlock()
	if prev, found := t.setStatus(name, StatusInactive); found && prev != StatusActive {
		return nil
	}
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.MuxSessionClosed,
		Data: eventbus.MuxHookEvent{SessionName: name},
	})
}

func (t *Service) setStatus(name string, status SessionStatus) (SessionStatus, bool) {
	defer t.lockName(name)()
	ts, err := t.store.GetByName(name)
	if err != nil {
		return "", false
	}
	prev := ts.Status
	if prev != status {
		ts.Status = status
		if err := t.store.Update(ts); err != nil {
			slog.Warn("failed to update mux session status", "mux", name, "status", status, "error", err)
		}
	}
	return prev, true
}

func (t *Service) HandleClientSessionChanged(ctx context.Context, name, host string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.MuxClientSessionChanged,
		Data: eventbus.MuxHookEvent{SessionName: name, Host: host},
	})
}

func (t *Service) HandleClientAttached(ctx context.Context, name, host string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.MuxClientAttached,
		Data: eventbus.MuxHookEvent{SessionName: name, Host: host},
	})
}

func (t *Service) HandleClientDetached(ctx context.Context, name, host string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.MuxClientDetached,
		Data: eventbus.MuxHookEvent{SessionName: name, Host: host},
	})
}

func (t *Service) SpawnWindow(sessionName, startDir, command string) error {
	var env map[string]string
	if ts, err := t.store.GetByName(sessionName); err == nil {
		env = ts.Env
	}
	return t.driver.NewWindow(sessionName, startDir, command, env)
}

func (t *Service) GetWindows(ctx context.Context, name string) []Window {
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	return t.windowsBySession[name]
}

func (t *Service) SetProcessLister(lister ProcessLister) {
	t.resources = newResourceSampler(lister, t.driver)
}

func (t *Service) ResourceUsage(ctx context.Context) ([]Usage, error) {
	return t.resources.sample(ctx)
}
