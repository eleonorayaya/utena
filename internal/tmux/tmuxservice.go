package tmux

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

var ErrTmuxNotAvailable = errors.New("tuios is not available")

var followedTuiosEvents = []string{
	"session-created",
	"session-closed",
	"window-created",
	"window-closed",
	"window-retitled",
	"window-focused",
	"window-moved",
	"workspace-switched",
	"gap",
}

type TmuxService struct {
	runner            tmuxRunner
	store             *TmuxStore
	eventBus          eventbus.EventBus
	windowsBySession  map[string][]Window
	windowsMu         sync.RWMutex
	nameLocks         sync.Map
	activationsMu     sync.Mutex
	activationWaiters []chan string
}

func (t *TmuxService) lockName(name string) func() {
	actual, _ := t.nameLocks.LoadOrStore(name, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func NewTmuxService(runner tmuxRunner, store *TmuxStore, bus eventbus.EventBus) *TmuxService {
	t := &TmuxService{
		runner:           runner,
		store:            store,
		eventBus:         bus,
		windowsBySession: make(map[string][]Window),
	}
	bus.Subscribe(eventbus.SessionActivated, t.handleSessionActivated)
	return t
}

func (t *TmuxService) OnAppStart(ctx context.Context) error {
	if err := t.store.BackfillStatus(); err != nil {
		slog.WarnContext(ctx, "tmux: backfill status from is_alive failed", "error", err)
	}
	if t.runner != nil {
		go t.followEvents(ctx)
	}
	return nil
}

func (t *TmuxService) followEvents(ctx context.Context) {
	lastErr := ""
	for ctx.Err() == nil {
		err := t.runner.subscribe(ctx, followedTuiosEvents, func(ev tuiosEvent) {
			lastErr = ""
			t.handleTuiosEvent(ctx, ev)
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil && err.Error() != lastErr {
			lastErr = err.Error()
			slog.Warn("tuios event stream unavailable; retrying", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (t *TmuxService) handleTuiosEvent(ctx context.Context, ev tuiosEvent) {
	var err error
	switch ev.Type {
	case tuiosEventSubscribed, "gap":
		t.resync()
	case "session-created":
		err = t.HandleSessionCreated(ctx, ev.Session)
		t.refreshWindows(ev.Session)
	case "session-closed":
		err = t.HandleSessionClosed(ctx, ev.Session)
	default:
		t.refreshWindows(ev.Session)
	}
	if err != nil {
		slog.Warn("tuios event handler failed", "type", ev.Type, "session", ev.Session, "error", err)
	}
}

func (t *TmuxService) resync() {
	names, err := t.runner.listSessionNames()
	if err != nil {
		slog.Warn("tuios resync: list-sessions failed", "error", err)
		return
	}
	for _, ts := range t.store.List() {
		alive := slices.Contains(names, ts.Name)
		switch {
		case alive && ts.Status == TmuxStatusInactive:
			ts.Status = TmuxStatusActive
		case !alive && ts.Status == TmuxStatusActive:
			ts.Status = TmuxStatusInactive
		default:
			continue
		}
		if err := t.store.Update(&ts); err != nil {
			slog.Warn("tuios resync: update failed", "tmux", ts.Name, "error", err)
		}
	}
	for _, name := range names {
		t.refreshWindows(name)
	}
}

func (t *TmuxService) refreshWindows(sessionName string) {
	if sessionName == "" {
		return
	}
	windows, err := t.runner.listWindows(sessionName)
	if err != nil {
		slog.Debug("tuios list-windows failed", "session", sessionName, "error", err)
		return
	}
	t.windowsMu.Lock()
	defer t.windowsMu.Unlock()
	t.windowsBySession[sessionName] = windows
}

func (t *TmuxService) handleSessionActivated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionActivatedEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}
	t.activationsMu.Lock()
	defer t.activationsMu.Unlock()
	for _, ch := range t.activationWaiters {
		ch <- data.SessionName
	}
	t.activationWaiters = nil
	return nil
}

func (t *TmuxService) WaitForActivation(ctx context.Context) (string, error) {
	ch := make(chan string, 1)
	t.activationsMu.Lock()
	t.activationWaiters = append(t.activationWaiters, ch)
	t.activationsMu.Unlock()

	select {
	case name := <-ch:
		return name, nil
	case <-ctx.Done():
		t.activationsMu.Lock()
		t.activationWaiters = slices.DeleteFunc(t.activationWaiters, func(c chan string) bool { return c == ch })
		t.activationsMu.Unlock()
		return "", ctx.Err()
	}
}

func (t *TmuxService) SessionEnv(name, key string) (string, bool) {
	ts, err := t.store.GetByName(name)
	if err != nil {
		return "", false
	}
	v, ok := ts.Env[key]
	return v, ok
}

func (t *TmuxService) OnAppEnd(ctx context.Context) error {
	return nil
}

// RegisterPending inserts a TmuxSession record in the pending state without
// spawning a tmux process. It is idempotent: if a record with that name already
// exists in pending or inactive status, the existing record is returned. If a
// record exists in active status, ErrTmuxSessionAlreadyExists is returned —
// the caller should not be re-registering an active session.
func (t *TmuxService) RegisterPending(name, startDir string, env map[string]string) (*TmuxSession, error) {
	defer t.lockName(name)()
	if existing, err := t.store.GetByName(name); err == nil {
		// Adopt whatever's there. The Session.TmuxSessionID unique constraint
		// will reject the caller's link if another session already owns this
		// tmux record — that's where real conflicts surface. If the previous
		// owner was deleted, its FK was nil'd and the record is unclaimed; the
		// new session adopts the running tmux process cleanly.
		return existing, nil
	} else if !errors.Is(err, ErrTmuxSessionNotFound) {
		return nil, err
	}
	ts := &TmuxSession{
		Name:     name,
		StartDir: startDir,
		Env:      env,
		Status:   TmuxStatusPending,
	}
	if err := t.store.Add(ts); err != nil {
		if errors.Is(err, ErrTmuxSessionAlreadyExists) {
			existing, getErr := t.store.GetByName(name)
			if getErr != nil {
				return nil, getErr
			}
			if existing.Status == TmuxStatusActive {
				return nil, ErrTmuxSessionAlreadyExists
			}
			return existing, nil
		}
		return nil, err
	}
	return ts, nil
}

// SpawnForRecord spawns the tmux process for an existing record and transitions
// it to active. The runner-newSession failure does not delete the record —
// callers can retry. Idempotent for the (rare) case the tmux session already
// exists by name: the record is just transitioned to active.
func (t *TmuxService) SpawnForRecord(id uint) (*TmuxSession, error) {
	if t.runner == nil {
		return nil, ErrTmuxNotAvailable
	}
	ts, err := t.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	defer t.lockName(ts.Name)()
	if !t.runner.hasSession(ts.Name) {
		if err := t.runner.newSession(ts.Name, ts.StartDir, ts.Env); err != nil {
			return nil, err
		}
	}
	ts.Status = TmuxStatusActive
	if err := t.store.Update(ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (t *TmuxService) CreateSession(name, startDir string, env map[string]string) (*TmuxSession, error) {
	if t.runner == nil {
		return nil, ErrTmuxNotAvailable
	}
	defer t.lockName(name)()
	if err := t.runner.newSession(name, startDir, env); err != nil {
		return nil, err
	}
	ts := &TmuxSession{
		Name:     name,
		StartDir: startDir,
		Env:      env,
		Status:   TmuxStatusActive,
	}
	if err := t.store.Add(ts); err != nil {
		if !errors.Is(err, ErrTmuxSessionAlreadyExists) {
			return nil, err
		}
		existing, getErr := t.store.GetByName(name)
		if getErr != nil {
			return nil, getErr
		}
		existing.StartDir = startDir
		existing.Env = env
		existing.Status = TmuxStatusActive
		if updateErr := t.store.Update(existing); updateErr != nil {
			return nil, updateErr
		}
		return existing, nil
	}
	return ts, nil
}

func (t *TmuxService) KillSession(id uint) error {
	if t.runner == nil {
		return ErrTmuxNotAvailable
	}
	ts, err := t.store.GetByID(id)
	if err != nil {
		return err
	}
	defer t.lockName(ts.Name)()
	killErr := t.runner.killSession(ts.Name)
	if killErr != nil {
		slog.Warn("tmux runner killSession failed; marking record inactive anyway", "name", ts.Name, "error", killErr)
	}
	ts.Status = TmuxStatusInactive
	return t.store.Update(ts)
}

func (t *TmuxService) HasSession(name string) bool {
	if t.runner == nil {
		return false
	}
	return t.runner.hasSession(name)
}

func (t *TmuxService) GetSession(id uint) (*TmuxSession, error) {
	ts, err := t.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	ts.Windows = t.windowsBySession[ts.Name]
	return ts, nil
}

func (t *TmuxService) GetSessionByName(name string) (*TmuxSession, error) {
	ts, err := t.store.GetByName(name)
	if err != nil {
		return nil, err
	}
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	ts.Windows = t.windowsBySession[ts.Name]
	return ts, nil
}

func (t *TmuxService) GetOrTrackSession(name, startDir string, env map[string]string) (*TmuxSession, error) {
	ts, err := t.store.GetByName(name)
	if err == nil {
		return ts, nil
	}
	if !errors.Is(err, ErrTmuxSessionNotFound) {
		return nil, err
	}
	ts = &TmuxSession{Name: name, StartDir: startDir, Env: env, Status: TmuxStatusActive}
	if err := t.store.Add(ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (t *TmuxService) HandleSessionCreated(ctx context.Context, tmuxName string) error {
	t.setStatus(tmuxName, TmuxStatusActive)
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxSessionCreated,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) HandleSessionClosed(ctx context.Context, tmuxName string) error {
	t.windowsMu.Lock()
	delete(t.windowsBySession, tmuxName)
	t.windowsMu.Unlock()
	if prev, found := t.setStatus(tmuxName, TmuxStatusInactive); found && prev != TmuxStatusActive {
		return nil
	}
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxSessionClosed,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) setStatus(tmuxName string, status TmuxSessionStatus) (TmuxSessionStatus, bool) {
	defer t.lockName(tmuxName)()
	ts, err := t.store.GetByName(tmuxName)
	if err != nil {
		return "", false
	}
	prev := ts.Status
	if prev != status {
		ts.Status = status
		if err := t.store.Update(ts); err != nil {
			slog.Warn("failed to update tmux session status", "tmux", tmuxName, "status", status, "error", err)
		}
	}
	return prev, true
}

func (t *TmuxService) HandleClientSessionChanged(ctx context.Context, tmuxName string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxClientSessionChanged,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) HandleClientAttached(ctx context.Context, tmuxName string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxClientAttached,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) HandleClientDetached(ctx context.Context, tmuxName string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxClientDetached,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) SpawnWindow(sessionName, startDir, command string) error {
	if t.runner == nil {
		return ErrTmuxNotAvailable
	}
	var env map[string]string
	if ts, err := t.store.GetByName(sessionName); err == nil {
		env = ts.Env
	}
	return t.runner.newWindow(sessionName, startDir, command, env)
}

func (t *TmuxService) GetWindows(ctx context.Context, tmuxName string) []Window {
	t.windowsMu.RLock()
	defer t.windowsMu.RUnlock()
	return t.windowsBySession[tmuxName]
}
