package tmux

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/eleonorayaya/utena/internal/eventbus"
)

var ErrTmuxNotAvailable = errors.New("tmux is not available")

type TmuxService struct {
	runner           tmuxRunner
	store            *TmuxStore
	eventBus         eventbus.EventBus
	windowsBySession map[string][]Window
	nameLocks        sync.Map
}

func (t *TmuxService) lockName(name string) func() {
	actual, _ := t.nameLocks.LoadOrStore(name, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func NewTmuxService(runner tmuxRunner, store *TmuxStore, bus eventbus.EventBus) *TmuxService {
	return &TmuxService{
		runner:           runner,
		store:            store,
		eventBus:         bus,
		windowsBySession: make(map[string][]Window),
	}
}

func (t *TmuxService) OnAppStart(ctx context.Context) error {
	return nil
}

func (t *TmuxService) OnAppEnd(ctx context.Context) error {
	return nil
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
		IsAlive:  true,
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
		existing.IsAlive = true
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
	if err := t.runner.killSession(ts.Name); err != nil {
		return err
	}
	ts.IsAlive = false
	return t.store.Update(ts)
}

func (t *TmuxService) KillSessionByName(name string) error {
	if t.runner == nil {
		return ErrTmuxNotAvailable
	}
	defer t.lockName(name)()
	if err := t.runner.killSession(name); err != nil {
		return err
	}
	ts, err := t.store.GetByName(name)
	if err != nil {
		if errors.Is(err, ErrTmuxSessionNotFound) {
			return nil
		}
		return err
	}
	ts.IsAlive = false
	return t.store.Update(ts)
}

func (t *TmuxService) RecreateSession(id uint) error {
	if t.runner == nil {
		return ErrTmuxNotAvailable
	}
	ts, err := t.store.GetByID(id)
	if err != nil {
		return err
	}
	defer t.lockName(ts.Name)()
	if err := t.runner.newSession(ts.Name, ts.StartDir, ts.Env); err != nil {
		return err
	}
	ts.IsAlive = true
	return t.store.Update(ts)
}

func (t *TmuxService) HasSession(name string) bool {
	if t.runner == nil {
		return false
	}
	return t.runner.hasSession(name)
}

func (t *TmuxService) SwitchClient(targetSession string) error {
	if t.runner == nil {
		return ErrTmuxNotAvailable
	}
	return t.runner.switchClient(targetSession)
}

func (t *TmuxService) GetCurrentSessionName(paneID string) (string, error) {
	if t.runner == nil {
		return "", ErrTmuxNotAvailable
	}
	return t.runner.command("display-message", "-p", "-t", paneID, "#{session_name}")
}

func (t *TmuxService) GetSession(id uint) (*TmuxSession, error) {
	ts, err := t.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	ts.Windows = t.windowsBySession[ts.Name]
	return ts, nil
}

func (t *TmuxService) GetSessionByName(name string) (*TmuxSession, error) {
	ts, err := t.store.GetByName(name)
	if err != nil {
		return nil, err
	}
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
	ts = &TmuxSession{Name: name, StartDir: startDir, Env: env, IsAlive: true}
	if err := t.store.Add(ts); err != nil {
		return nil, err
	}
	return ts, nil
}

func (t *TmuxService) ListSessionNames() ([]string, error) {
	if t.runner == nil {
		return nil, ErrTmuxNotAvailable
	}
	return t.runner.listSessionNames()
}

func (t *TmuxService) HandleSessionCreated(ctx context.Context, tmuxName string) error {
	ts, err := t.store.GetByName(tmuxName)
	if err == nil {
		ts.IsAlive = true
		if updateErr := t.store.Update(ts); updateErr != nil {
			slog.Warn("failed to mark tmux session alive", "tmux", tmuxName, "error", updateErr)
		}
	}
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxSessionCreated,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) HandleSessionClosed(ctx context.Context, tmuxName string) error {
	ts, err := t.store.GetByName(tmuxName)
	if err == nil {
		ts.IsAlive = false
		if updateErr := t.store.Update(ts); updateErr != nil {
			slog.Warn("failed to mark tmux session dead", "tmux", tmuxName, "error", updateErr)
		}
	}
	delete(t.windowsBySession, tmuxName)
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxSessionClosed,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
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
	return t.runner.newWindow(sessionName, startDir, command)
}

func (t *TmuxService) SyncWindows(ctx context.Context, tmuxName string, windows []Window) {
	t.windowsBySession[tmuxName] = windows
}

func (t *TmuxService) GetWindows(ctx context.Context, tmuxName string) []Window {
	return t.windowsBySession[tmuxName]
}
