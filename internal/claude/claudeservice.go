package claude

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/eleonorayaya/utena/internal/eventbus"
)

type ClaudeService struct {
	store    *ClaudeStore
	eventBus eventbus.EventBus
}

func NewClaudeService(store *ClaudeStore, bus eventbus.EventBus) *ClaudeService {
	return &ClaudeService{
		store:    store,
		eventBus: bus,
	}
}

func (s *ClaudeService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.SessionActivated, s.handleSessionActivated)
	s.eventBus.Subscribe(eventbus.TmuxClientSessionChanged, s.handleTmuxClientSessionChanged)
	return nil
}

func (s *ClaudeService) handleSessionActivated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.SessionActivatedEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	s.store.UpdateStatusBySessionID(data.SessionName, StatusReadyForReview, StatusCompleted)
	return nil
}

func (s *ClaudeService) handleTmuxClientSessionChanged(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}
	s.store.UpdateStatusBySessionID(data.TmuxSessionName, StatusReadyForReview, StatusCompleted)
	return nil
}

func (s *ClaudeService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *ClaudeService) HandleHookEvent(ctx context.Context, req *HookEventRequest) error {
	switch req.Event {
	case "SessionStart", "UserPromptSubmit":
		return s.upsertWithStatus(req, StatusWorking)

	case "Stop":
		return s.upsertWithStatus(req, StatusReadyForReview)

	case "Notification":
		if req.NotificationType == "permission_prompt" {
			return s.upsertWithStatus(req, StatusNeedsAttention)
		}
		return nil

	case "PreToolUse":
		return s.store.UpdateStatusByClaudeSessionID(req.ClaudeSessionID, StatusWorking)

	case "TaskCompleted":
		return s.upsertWithStatus(req, StatusReadyForReview)

	case "SessionEnd":
		err := s.store.DeleteByClaudeSessionID(req.ClaudeSessionID)
		if err != nil {
			slog.Warn("failed to delete claude session on SessionEnd", "claude_session_id", req.ClaudeSessionID, "error", err)
		}
		return nil

	default:
		slog.Warn("unknown hook event", "event", req.Event)
		return nil
	}
}

func (s *ClaudeService) upsertWithStatus(req *HookEventRequest, status ClaudeSessionStatus) error {
	session := &ClaudeSession{
		ClaudeSessionID: req.ClaudeSessionID,
		SessionID:       req.SessionID,
		Status:          status,
		CWD:             req.CWD,
	}
	return s.store.Upsert(session)
}

func (s *ClaudeService) ListAll(ctx context.Context) ([]ClaudeSession, error) {
	return s.store.List(), nil
}

func (s *ClaudeService) ListBySession(ctx context.Context, sessionID string) ([]ClaudeSession, error) {
	return s.store.ListBySessionID(sessionID), nil
}
