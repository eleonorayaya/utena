package zellij

import (
	"context"
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
	z.eventBus.Subscribe(eventbus.SessionCreateRequested, z.handleSessionCreateRequested)
	z.eventBus.Subscribe(eventbus.SessionActivated, z.handleSessionActivated)
	return nil
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
			sess.IsAttached = update.IsCurrentSession
			sess.IsActive = true
			sess.IsDead = false
			sess.LastUsedAt = time.Now()
			delete(activeSessions, sess.ID)
		} else {
			sess.IsDead = true
		}

		if err := z.sessionService.UpdateSession(ctx, &sess); err != nil {
			return err
		}
	}

	for sessionID, sessionUpdate := range activeSessions {
		newSession := &session.Session{
			ID:          sessionID,
			WorkspaceID: "",
			IsAttached:  sessionUpdate.IsCurrentSession,
			IsActive:    true,
			IsDead:      false,
			LastUsedAt:  time.Now(),
		}

		if err := z.sessionService.CreateSession(ctx, newSession); err != nil {
			return err
		}
	}

	return nil
}

func (z *ZellijService) handleSessionCreateRequested(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionCreateRequestedEvent)
	if !ok {
		return nil
	}
	return z.CreateSession(data.SessionName, data.WorkspacePath)
}

func (z *ZellijService) handleSessionActivated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionActivatedEvent)
	if !ok {
		return nil
	}
	return z.SwitchSession(data.SessionName)
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

func (z *ZellijService) SwitchSession(sessionName string) error {
	cmd := Command{
		Command:     "switch_session",
		SessionName: &sessionName,
	}
	return z.sendCommandToPlugin(cmd)
}

func (z *ZellijService) CreateSession(sessionName, workspacePath string) error {
	cmd := Command{
		Command:       "create_session",
		SessionName:   &sessionName,
		WorkspacePath: &workspacePath,
	}
	return z.sendCommandToPlugin(cmd)
}

func (z *ZellijService) ClosePicker() error {
	cmd := Command{
		Command: "close_picker",
	}
	return z.sendCommandToPlugin(cmd)
}
