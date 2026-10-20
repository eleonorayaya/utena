package browser

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
)

const followerOpenTimeout = 15 * time.Second

type LeaderSessions interface {
	LeaderSessionID(ctx context.Context, sessionID uint) (*uint, error)
}

type FollowerModule struct {
	leaderURL string
	client    *http.Client
	sessions  LeaderSessions
}

func NewFollowerModule(leaderURL string, client *http.Client, sessions LeaderSessions) *FollowerModule {
	if client == nil {
		client = &http.Client{}
	}
	return &FollowerModule{leaderURL: strings.TrimRight(leaderURL, "/"), client: client, sessions: sessions}
}

func (m *FollowerModule) OnAppStart(context.Context) error { return nil }

func (m *FollowerModule) OnAppEnd(context.Context) error { return nil }

func (m *FollowerModule) Models() []any {
	return []any{&BrowserWindow{}, &BrowserState{}}
}

func (m *FollowerModule) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(rejectCrossOrigin)
	r.Post("/open", m.handleOpen)
	r.Get("/status", m.handleStatus)
	r.Get("/sessions", m.handleSessions)
	r.Post("/focus", m.handleFocus)
	return r
}

func (m *FollowerModule) Open(ctx context.Context, req OpenRequest) (OpenResult, error) {
	if req.SessionID != 0 {
		leaderID, err := m.sessions.LeaderSessionID(ctx, req.SessionID)
		switch {
		case errors.Is(err, ErrSessionNotFound):
			req.SessionID = 0
		case err != nil:
			return OpenResult{}, err
		case leaderID == nil:
			return OpenResult{}, common.NewConflict(fmt.Sprintf("session %d has no leader session id", req.SessionID))
		default:
			req.SessionID = *leaderID
		}
	}

	res, err := RequestOpen(ctx, m.client, m.leaderURL+"/browser", req)
	if err != nil {
		var appErr *common.AppError
		if errors.As(err, &appErr) {
			return OpenResult{}, err
		}
		return OpenResult{}, &common.AppError{Category: common.CategoryUnavailable, Message: "open on leader failed", Err: err}
	}
	return res, nil
}

func (m *FollowerModule) handleOpen(w http.ResponseWriter, r *http.Request) {
	req := &OpenRequest{}
	if err := render.Bind(r, req); err != nil {
		common.RenderError(w, r, common.NewInvalidRequest(err.Error()))
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), followerOpenTimeout)
	defer cancel()

	res, err := m.Open(ctx, *req)
	if err != nil {
		common.RenderError(w, r, err)
		return
	}
	render.JSON(w, r, res)
}

func (m *FollowerModule) handleStatus(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, map[string]bool{"connected": false, "outdated": false})
}

func (m *FollowerModule) handleSessions(w http.ResponseWriter, r *http.Request) {
	render.JSON(w, r, SessionsView{Sessions: map[uint]SessionBrowser{}})
}

func (m *FollowerModule) handleFocus(w http.ResponseWriter, r *http.Request) {
	common.RenderError(w, r, ErrUnavailable)
}
