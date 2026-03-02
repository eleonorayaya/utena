package tmux

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
)

type TmuxService struct {
	sessionService *session.SessionService
	eventBus       eventbus.EventBus
}

func NewTmuxService(sessionService *session.SessionService, bus eventbus.EventBus) *TmuxService {
	return &TmuxService{
		sessionService: sessionService,
		eventBus:       bus,
	}
}

func (t *TmuxService) OnAppStart(ctx context.Context) error {
	t.eventBus.Subscribe(eventbus.SessionCreateRequested, t.handleSessionCreateRequested)
	t.eventBus.Subscribe(session.EventSessionDeleted, t.handleSessionDeleted)
	t.eventBus.Subscribe(eventbus.SessionRevived, t.handleSessionRevived)
	return nil
}

func (t *TmuxService) handleSessionCreateRequested(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionCreateRequestedEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := t.sessionService.GetSession(ctx, data.SessionName)
	if err != nil {
		slog.Warn("session not found for create event", "session", data.SessionName, "error", err)
		return nil
	}

	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", sess.TmuxSessionName, "-c", data.WorkspacePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("failed to create tmux session", "session", sess.TmuxSessionName, "error", err, "output", string(output))
	}

	return nil
}

func (t *TmuxService) handleSessionDeleted(ctx context.Context, event eventbus.Event) error {
	sess, ok := event.Data.(*session.Session)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	cmd := exec.CommandContext(ctx, "tmux", "kill-session", "-t", sess.TmuxSessionName)
	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("failed to kill tmux session", "session", sess.TmuxSessionName, "error", err, "output", string(output))
	}

	sess.Cleanup.TmuxSessionKilled = true
	return t.sessionService.UpdateSession(ctx, sess)
}

func (t *TmuxService) handleSessionRevived(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionRevivedEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := t.sessionService.GetSession(ctx, data.SessionName)
	if err != nil {
		slog.Warn("session not found for revive event", "session", data.SessionName, "error", err)
		return nil
	}

	cmd := exec.CommandContext(ctx, "tmux", "new-session", "-d", "-s", sess.TmuxSessionName, "-c", data.WorkspacePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("failed to create tmux session", "session", sess.TmuxSessionName, "error", err, "output", string(output))
	}

	return nil
}

func (t *TmuxService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (t *TmuxService) HandleSessionCreated(ctx context.Context, tmuxName string) error {
	sess, err := t.sessionService.GetSessionByTmuxName(ctx, tmuxName)
	if err != nil {
		slog.Debug("session not found for session-created hook", "tmux_name", tmuxName, "error", err)
		return nil
	}

	sess.IsActive = true
	sess.IsDead = false
	return t.sessionService.UpdateSession(ctx, sess)
}

func (t *TmuxService) HandleSessionClosed(ctx context.Context, tmuxName string) error {
	sess, err := t.sessionService.GetSessionByTmuxName(ctx, tmuxName)
	if err != nil {
		slog.Debug("session not found for session-closed hook", "tmux_name", tmuxName, "error", err)
		return nil
	}

	sess.IsDead = true
	sess.IsActive = false
	sess.IsAttached = false
	return t.sessionService.UpdateSession(ctx, sess)
}

func (t *TmuxService) HandleClientSessionChanged(ctx context.Context, tmuxName string) error {
	sessions, err := t.sessionService.ListSessions(ctx)
	if err != nil {
		return err
	}

	for _, s := range sessions {
		if s.IsAttached {
			s.IsAttached = false
			if err := t.sessionService.UpdateSession(ctx, &s); err != nil {
				return err
			}
		}
	}

	sess, err := t.sessionService.GetSessionByTmuxName(ctx, tmuxName)
	if err != nil {
		slog.Debug("session not found for client-session-changed hook", "tmux_name", tmuxName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	return t.sessionService.UpdateSession(ctx, sess)
}

func (t *TmuxService) HandleClientAttached(ctx context.Context, tmuxName string) error {
	sess, err := t.sessionService.GetSessionByTmuxName(ctx, tmuxName)
	if err != nil {
		slog.Debug("session not found for client-attached hook", "tmux_name", tmuxName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	return t.sessionService.UpdateSession(ctx, sess)
}

func (t *TmuxService) HandleClientDetached(ctx context.Context, tmuxName string) error {
	sess, err := t.sessionService.GetSessionByTmuxName(ctx, tmuxName)
	if err != nil {
		slog.Debug("session not found for client-detached hook", "tmux_name", tmuxName, "error", err)
		return nil
	}

	sess.IsAttached = false
	return t.sessionService.UpdateSession(ctx, sess)
}
