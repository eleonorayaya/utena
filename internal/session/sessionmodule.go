package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/jobs"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
)

type SessionModule struct {
	Store      *SessionStore
	Service    *SessionService
	Controller *SessionController
	Router     *SessionRouter
}

func NewSessionModule(tmuxService *utmux.TmuxService, workspaceModule *workspace.WorkspaceModule, bus eventbus.EventBus, database db.Database, branchPrefix string, configDir string, sessionsRoot string) *SessionModule {
	store := NewSessionStore(database)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	setupStepStore := NewSessionSetupStepStore(database)
	service := NewSessionService(store, sessionWorktreeStore, dismissedPRStore, sessionActionStore, setupStepStore, workspaceModule.Service, workspaceModule.GitService, tmuxService, bus, branchPrefix, configDir, sessionsRoot)
	controller := NewSessionController(service)
	router := NewSessionRouter(controller)

	return &SessionModule{
		Store:      store,
		Service:    service,
		Controller: controller,
		Router:     router,
	}
}

func (m *SessionModule) OnAppStart(ctx context.Context) error {
	if err := m.Store.OnAppStart(ctx); err != nil {
		return err
	}

	if err := m.Service.OnAppStart(ctx); err != nil {
		return err
	}

	return nil
}

func (m *SessionModule) OnAppEnd(ctx context.Context) error {
	if err := m.Service.OnAppEnd(ctx); err != nil {
		return err
	}

	if err := m.Store.OnAppEnd(ctx); err != nil {
		return err
	}

	return nil
}

func (m *SessionModule) Models() []any {
	return []any{&Session{}, &SessionWorktree{}, &DismissedPR{}, &SessionAction{}, &SessionSetupStep{}}
}

func (m *SessionModule) Routes() chi.Router {
	return m.Router.Routes()
}

type reconcileSyncTask struct {
	service *SessionService
}

func (t *reconcileSyncTask) Name() string            { return "session.reconcile" }
func (t *reconcileSyncTask) Interval() time.Duration { return 1 * time.Minute }
func (t *reconcileSyncTask) Run(ctx context.Context) error {
	sessions, err := t.service.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	for _, sess := range sessions {
		if sess.Status != StatusDeleted && sess.Status != StatusArchived && sess.Status != StatusCreating {
			if _, err := t.service.ReconcileSession(ctx, sess.ID); err != nil {
				slog.Warn("reconcile failed for session", "session", sess.ID, "error", err)
			}
		}
	}
	return nil
}

type completedCleanupTask struct {
	service *SessionService
}

func (t *completedCleanupTask) Name() string            { return "session.completed_cleanup" }
func (t *completedCleanupTask) Interval() time.Duration { return 5 * time.Minute }
func (t *completedCleanupTask) Run(ctx context.Context) error {
	sessions, err := t.service.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	cutoff := time.Now().Add(-5 * time.Minute)
	for _, sess := range sessions {
		if sess.Status == StatusCompleted && !sess.IsAttached && sess.LastUsedAt.Before(cutoff) {
			if _, err := t.service.ArchiveSession(ctx, sess.ID); err != nil {
				slog.Warn("failed to auto-archive completed session", "session", sess.ID, "error", err)
			} else {
				slog.Info("auto-archived completed session", "session", sess.ID)
			}
		}
	}
	return nil
}

func (m *SessionModule) RegisterJobs(svc *jobs.JobService) {
	svc.Register(&reconcileSyncTask{service: m.Service})
	svc.Register(&completedCleanupTask{service: m.Service})
}
