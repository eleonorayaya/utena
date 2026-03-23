package claude

import (
	"context"
	"errors"
	"log/slog"
)

type ClaudeService struct {
	store *ClaudeStore
}

func NewClaudeService(store *ClaudeStore) *ClaudeService {
	return &ClaudeService{store: store}
}

func (s *ClaudeService) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *ClaudeService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *ClaudeService) HandleHookEvent(ctx context.Context, req *HookEventRequest) error {
	switch req.Event {
	case "SessionStart":
		if req.SessionID == 0 {
			return errors.New("session_id is required for SessionStart")
		}
		return s.store.Create(&ClaudeSession{
			ClaudeSessionID: req.ClaudeSessionID,
			SessionID:       req.SessionID,
			Status:          StatusIdle,
			CWD:             req.CWD,
		})
	case "UserPromptSubmit", "PreToolUse":
		return s.store.UpdateStatus(req.ClaudeSessionID, StatusWorking)
	case "Stop", "TaskCompleted":
		return s.store.UpdateStatus(req.ClaudeSessionID, StatusReadyForReview)
	case "Notification":
		if req.NotificationType == "permission_prompt" {
			return s.store.UpdateStatus(req.ClaudeSessionID, StatusNeedsAttention)
		}
		return nil
	case "SessionEnd":
		return s.store.UpdateStatus(req.ClaudeSessionID, StatusDone)
	default:
		slog.Warn("unknown hook event", "event", req.Event)
		return nil
	}
}

func (s *ClaudeService) ListAll(ctx context.Context) ([]ClaudeSession, error) {
	return s.store.List(), nil
}
