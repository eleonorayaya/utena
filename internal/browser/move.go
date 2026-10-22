package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
)

type MoveTabRequest struct {
	SessionID   uint   `json:"session_id"`
	WindowID    string `json:"window_id"`
	TabID       string `json:"tab_id"`
	ToSessionID uint   `json:"to_session_id"`
}

func (r *MoveTabRequest) Bind(*http.Request) error {
	switch {
	case r.SessionID == 0:
		return errors.New("session_id is required")
	case r.WindowID == "":
		return errors.New("window_id is required")
	case r.TabID == "":
		return errors.New("tab_id is required")
	case r.ToSessionID == 0:
		return errors.New("to_session_id is required")
	case r.ToSessionID == r.SessionID:
		return errors.New("to_session_id must differ from session_id")
	}
	return nil
}

type MoveTabResult struct {
	WindowID string `json:"window_id"`
	Created  bool   `json:"created"`
}

func (s *Service) MoveTab(ctx context.Context, req MoveTabRequest) (MoveTabResult, error) {
	if !s.Connected() {
		return MoveTabResult{}, ErrUnavailable
	}
	if _, err := s.sessions.IsAttached(ctx, req.ToSessionID); errors.Is(err, ErrSessionNotFound) {
		return MoveTabResult{}, common.WrapNotFound(fmt.Sprintf("session %d", req.ToSessionID), err)
	} else if err != nil {
		return MoveTabResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected.Load() {
		return MoveTabResult{}, ErrUnavailable
	}
	_, live, err := s.focusTarget(ctx, FocusRequest{SessionID: req.SessionID, WindowID: req.WindowID, TabID: req.TabID})
	if err != nil {
		return MoveTabResult{}, err
	}
	target, found, err := s.targetWindow(req.ToSessionID, live)
	if err != nil {
		return MoveTabResult{}, err
	}

	if !found {
		var w Window
		if _, err := s.keepFocus(ctx, func() (bool, error) {
			var err error
			w, err = s.createSessionWindow(ctx, req.ToSessionID, live, func() (Window, error) {
				return s.driver.DetachTab(ctx, req.WindowID, req.TabID)
			})
			return err == nil, err
		}); err != nil {
			return MoveTabResult{}, err
		}
		s.signal()
		return MoveTabResult{WindowID: w.ID, Created: true}, nil
	}

	if err := s.driver.MoveTab(ctx, req.WindowID, req.TabID, target.ID); err != nil {
		return MoveTabResult{}, driverErr(err)
	}
	if err := s.driver.ActivateTab(ctx, target.ID, req.TabID); err != nil {
		slog.WarnContext(ctx, "browser: activate moved tab failed", "window", target.ID, "tab", req.TabID, "error", err)
	}
	return MoveTabResult{WindowID: target.ID}, nil
}
