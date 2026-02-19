package claude

import (
	"context"
	"log/slog"
	"time"
)

type ClaudeService struct {
	store *ClaudeStore
}

func NewClaudeService(store *ClaudeStore) *ClaudeService {
	return &ClaudeService{
		store: store,
	}
}

func (s *ClaudeService) OnAppStart(ctx context.Context) error {
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
		return s.upsertWithStatus(req, StatusCompleted)

	case "Notification":
		if req.NotificationType == "permission_prompt" {
			return s.upsertWithStatus(req, StatusNeedsAttention)
		}
		return nil

	case "TaskCompleted":
		return s.upsertWithStatus(req, StatusCompleted)

	case "SessionEnd":
		err := s.store.Delete(req.ClaudeSessionID)
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
		ID:            req.ClaudeSessionID,
		SessionID:     req.SessionID,
		Status:        status,
		CWD:           req.CWD,
		LastUpdatedAt: time.Now(),
	}
	return s.store.Upsert(session)
}

func (s *ClaudeService) ListAll(ctx context.Context) ([]ClaudeSession, error) {
	return s.store.List(), nil
}

func (s *ClaudeService) ListBySession(ctx context.Context, sessionID string) ([]ClaudeSession, error) {
	return s.store.ListBySessionID(sessionID), nil
}
