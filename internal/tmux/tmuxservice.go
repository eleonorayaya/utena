package tmux

import (
	"context"
	"log/slog"

	"github.com/GianlucaP106/gotmux/gotmux"
	"github.com/eleonorayaya/utena/internal/eventbus"
)

type TmuxService struct {
	tmux             *gotmux.Tmux
	eventBus         eventbus.EventBus
	windowsBySession map[string][]Window
}

func NewTmuxService(bus eventbus.EventBus) *TmuxService {
	tmux, err := gotmux.DefaultTmux()
	if err != nil {
		slog.Warn("failed to initialize gotmux", "error", err)
	}

	return &TmuxService{
		tmux:             tmux,
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

func (t *TmuxService) CreateSession(name, startDir string) error {
	opts := &gotmux.SessionOptions{
		Name:           name,
		StartDirectory: startDir,
	}
	_, err := t.tmux.NewSession(opts)
	return err
}

func (t *TmuxService) KillSession(name string) error {
	sess, err := t.tmux.GetSessionByName(name)
	if err != nil {
		return err
	}
	return sess.Kill()
}

func (t *TmuxService) HasSession(name string) bool {
	return t.tmux.HasSession(name)
}

func (t *TmuxService) ListSessionNames() ([]string, error) {
	sessions, err := t.tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (t *TmuxService) SwitchClient(targetSession string) error {
	return t.tmux.SwitchClient(&gotmux.SwitchClientOptions{
		TargetSession: targetSession,
	})
}

func (t *TmuxService) GetCurrentSessionName(paneID string) (string, error) {
	output, err := t.tmux.Command("display-message", "-p", "-t", paneID, "#{session_name}")
	if err != nil {
		return "", err
	}
	return output, nil
}

func (t *TmuxService) HandleSessionCreated(ctx context.Context, tmuxName string) error {
	return t.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.TmuxSessionCreated,
		Data: eventbus.TmuxHookEvent{TmuxSessionName: tmuxName},
	})
}

func (t *TmuxService) HandleSessionClosed(ctx context.Context, tmuxName string) error {
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

func (t *TmuxService) SyncWindows(ctx context.Context, tmuxName string, windows []Window) {
	t.windowsBySession[tmuxName] = windows
}

func (t *TmuxService) GetWindows(ctx context.Context, tmuxName string) []Window {
	return t.windowsBySession[tmuxName]
}
