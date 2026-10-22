package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/wm"
)

type EjectRequest struct {
	SessionID uint   `json:"session_id"`
	WindowID  string `json:"window_id"`
}

func (r *EjectRequest) Bind(*http.Request) error {
	switch {
	case r.SessionID == 0:
		return errors.New("session_id is required")
	case r.WindowID == "":
		return errors.New("window_id is required")
	}
	return nil
}

type EjectResult struct {
	WindowID string `json:"window_id"`
	Moved    bool   `json:"moved"`
}

func (s *Service) Eject(ctx context.Context, req EjectRequest) (EjectResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.connected.Load() {
		return EjectResult{}, ErrUnavailable
	}

	row, err := s.store.GetByDriverWindowID(req.WindowID)
	if errors.Is(err, ErrWindowNotFound) || (err == nil && row.SessionID != req.SessionID) {
		return EjectResult{}, common.NewInvalidRequest(fmt.Sprintf("window %s is not an open window of session %d", req.WindowID, req.SessionID))
	}
	if err != nil {
		return EjectResult{}, err
	}
	live, err := s.driver.ListWindows(ctx)
	if err != nil {
		return EjectResult{}, driverErr(err)
	}
	if _, ok := findWindow(live, req.WindowID); !ok {
		return EjectResult{}, ErrWindowNotFound
	}

	wmTarget, wmFound := s.wmFocusTarget(ctx, func(wmWindows []wm.Window) (string, bool) {
		wmID, ok := s.links.resolve(live, s.driver.WindowTitle, wmWindows, []string{req.WindowID})[req.WindowID]
		return wmID, ok
	})
	if err := s.store.Delete(row.ID); err != nil {
		return EjectResult{}, err
	}
	stopTimer(s.snapshots, req.WindowID)
	stopTimer(s.removals, req.WindowID)
	delete(s.links.links, req.WindowID)

	res := EjectResult{WindowID: req.WindowID}
	if wmFound && wmTarget.Workspace != s.layout.BrowserWorkspace {
		if err := s.wm.MoveToWorkspace(ctx, wmTarget.ID, s.layout.BrowserWorkspace); err != nil {
			slog.WarnContext(ctx, "browser: move ejected window failed", "window", req.WindowID, "wm_window", wmTarget.ID, "error", err)
		} else {
			res.Moved = true
		}
	}
	return res, nil
}
