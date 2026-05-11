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
	if err := t.store.BackfillStatus(); err != nil {
		slog.WarnContext(ctx, "tmux: backfill status from is_alive failed", "error", err)
	}
	return nil
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
	ts.Status = TmuxStatusInactive
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
	ts.Status = TmuxStatusActive
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
	ts = &TmuxSession{Name: name, StartDir: startDir, Env: env, Status: TmuxStatusActive}
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
		ts.Status = TmuxStatusActive
		if updateErr := t.store.Update(ts); updateErr != nil {
			slog.Warn("failed to mark tmux session active", "tmux", tmuxName, "error", updateErr)
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
		ts.Status = TmuxStatusInactive
		if updateErr := t.store.Update(ts); updateErr != nil {
			slog.Warn("failed to mark tmux session inactive", "tmux", tmuxName, "error", updateErr)
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
