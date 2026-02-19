package zellij

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/session"
)

type ZellijService struct {
	sessionService *session.SessionService
	eventBus       eventbus.EventBus
	pipeSender     *PipeSender
}

func NewZellijService(sessionService *session.SessionService, bus eventbus.EventBus) *ZellijService {
	return &ZellijService{
		sessionService: sessionService,
		eventBus:       bus,
		pipeSender:     NewPipeSender(),
	}
}

func (z *ZellijService) OnAppStart(ctx context.Context) error {
	z.eventBus.Subscribe(session.EventSessionDeleted, z.handleSessionDeleted)
	return nil
}

func (z *ZellijService) handleSessionDeleted(ctx context.Context, event eventbus.Event) error {
	sess, ok := event.Data.(*session.Session)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	if err := z.KillSession(ctx, sess.ID); err != nil {
		return err
	}

	sess.Cleanup.ZellijSessionKilled = true
	return z.sessionService.UpdateSession(ctx, sess)
}

func (z *ZellijService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (z *ZellijService) ProcessSessionUpdate(ctx context.Context, req *UpdateSessionsRequest) error {
	activeSessions := make(map[string]SessionUpdate)
	for _, sessionUpdate := range req.Sessions {
		activeSessions[sessionUpdate.Name] = sessionUpdate
	}

	allSessions, err := z.sessionService.ListSessions(ctx)
	if err != nil {
		return err
	}

	for _, existingSession := range allSessions {
		sess := existingSession

		if update, exists := activeSessions[sess.ID]; exists {
			sess.IsAttached = update.ConnectedClients > 0
			sess.IsActive = true
			sess.IsDead = false
			if sess.IsAttached {
				sess.LastUsedAt = time.Now()
			}
			delete(activeSessions, sess.ID)
		} else {
			sess.IsDead = true
		}

		if err := z.sessionService.UpdateSession(ctx, &sess); err != nil {
			return err
		}
	}

	for sessionID, sessionUpdate := range activeSessions {
		attached := sessionUpdate.ConnectedClients > 0
		newSession := &session.Session{
			ID:         sessionID,
			IsAttached: attached,
			IsActive:   true,
			IsDead:     false,
		}
		if attached {
			newSession.LastUsedAt = time.Now()
		}

		if err := z.sessionService.CreateSession(ctx, newSession); err != nil {
			return err
		}
	}

	return nil
}

func (z *ZellijService) KillSession(ctx context.Context, name string) error {
	cmd := exec.CommandContext(ctx, "zellij", "kill-session", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		slog.Warn("failed to kill zellij session", "session", name, "error", err, "output", string(output))
		return fmt.Errorf("zellij kill-session failed: %s: %w", string(output), err)
	}
	return nil
}

func (z *ZellijService) sendCommandToPlugin(command Command) error {
	return z.pipeSender.SendCommand(command)
}

func (z *ZellijService) OpenPicker() error {
	cmd := Command{
		Command: "open_picker",
	}
	return z.sendCommandToPlugin(cmd)
}

func (z *ZellijService) ClosePicker() error {
	cmd := Command{
		Command: "close_picker",
	}
	return z.sendCommandToPlugin(cmd)
}
