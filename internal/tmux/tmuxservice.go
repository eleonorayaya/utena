package tmux

import (
	"context"
	"errors"
	"log/slog"

	"github.com/GianlucaP106/gotmux/gotmux"
	"github.com/eleonorayaya/utena/internal/eventbus"
)

var ErrTmuxNotAvailable = errors.New("tmux is not available")

type TmuxClient interface {
	CreateSession(name, startDir string) error
	KillSession(name string) error
	HasSession(name string) bool
	ListSessionNames() ([]string, error)
	SwitchClient(targetSession string) error
	RunCommand(cmd ...string) (string, error)
}

type gotmuxClient struct {
	tmux *gotmux.Tmux
}

func NewGotmuxClient() TmuxClient {
	t, err := gotmux.DefaultTmux()
	if err != nil {
		slog.Warn("failed to initialize gotmux", "error", err)
		return nil
	}
	return &gotmuxClient{tmux: t}
}

func (c *gotmuxClient) CreateSession(name, startDir string) error {
	_, err := c.tmux.NewSession(&gotmux.SessionOptions{
		Name:           name,
		StartDirectory: startDir,
	})
	return err
}

func (c *gotmuxClient) KillSession(name string) error {
	sess, err := c.tmux.GetSessionByName(name)
	if err != nil {
		return err
	}
	return sess.Kill()
}

func (c *gotmuxClient) HasSession(name string) bool {
	return c.tmux.HasSession(name)
}

func (c *gotmuxClient) ListSessionNames() ([]string, error) {
	sessions, err := c.tmux.ListSessions()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(sessions))
	for i, s := range sessions {
		names[i] = s.Name
	}
	return names, nil
}

func (c *gotmuxClient) SwitchClient(targetSession string) error {
	return c.tmux.SwitchClient(&gotmux.SwitchClientOptions{
		TargetSession: targetSession,
	})
}

func (c *gotmuxClient) RunCommand(cmd ...string) (string, error) {
	return c.tmux.Command(cmd...)
}

type TmuxService struct {
	client           TmuxClient
	eventBus         eventbus.EventBus
	windowsBySession map[string][]Window
}

func NewTmuxService(client TmuxClient, bus eventbus.EventBus) *TmuxService {
	return &TmuxService{
		client:           client,
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
	if t.client == nil {
		return ErrTmuxNotAvailable
	}
	return t.client.CreateSession(name, startDir)
}

func (t *TmuxService) KillSession(name string) error {
	if t.client == nil {
		return ErrTmuxNotAvailable
	}
	return t.client.KillSession(name)
}

func (t *TmuxService) HasSession(name string) bool {
	if t.client == nil {
		return false
	}
	return t.client.HasSession(name)
}

func (t *TmuxService) ListSessionNames() ([]string, error) {
	if t.client == nil {
		return nil, ErrTmuxNotAvailable
	}
	return t.client.ListSessionNames()
}

func (t *TmuxService) SwitchClient(targetSession string) error {
	if t.client == nil {
		return ErrTmuxNotAvailable
	}
	return t.client.SwitchClient(targetSession)
}

func (t *TmuxService) GetCurrentSessionName(paneID string) (string, error) {
	if t.client == nil {
		return "", ErrTmuxNotAvailable
	}
	return t.client.RunCommand("display-message", "-p", "-t", paneID, "#{session_name}")
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
