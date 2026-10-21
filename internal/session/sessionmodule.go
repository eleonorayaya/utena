package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/jobs"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

type SessionModule struct {
	Store          *SessionStore
	Service        *SessionService
	Controller     *SessionController
	Router         *SessionRouter
	githubDisabled bool
}

func (m *SessionModule) DisableGitHub() {
	m.githubDisabled = true
}

func NewSessionModule(muxService *mux.Service, workspaceModule *workspace.WorkspaceModule, bus eventbus.EventBus, database db.Database, branchPrefix string, configDir string, sessionsRoot string) *SessionModule {
	store := NewSessionStore(database)
	dismissedPRStore := NewDismissedPRStore(database)
	sessionActionStore := NewSessionActionStore(database)
	sessionWorktreeStore := NewSessionWorktreeStore(database)
	setupStepStore := NewSessionSetupStepStore(database)
	service := NewSessionService(store, sessionWorktreeStore, dismissedPRStore, sessionActionStore, setupStepStore, workspaceModule.Service, workspaceModule.GitService, muxService, bus, branchPrefix, configDir, sessionsRoot)
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
	return []any{&Session{}, &SessionWorktree{}, &SessionBranch{}, &DismissedPR{}, &SessionAction{}, &SessionSetupStep{}, &SessionBlocker{}}
}

func (m *SessionModule) PreMigrations() []db.PreMigration {
	return []db.PreMigration{renameTmuxSessionColumn, dropTmuxSessionNameColumn, renameSetupTmuxSteps, cascadeClaudeSessions}
}

func cascadeClaudeSessions(tx *gorm.DB) error {
	mg := tx.Migrator()
	if !mg.HasTable("claude_sessions") {
		return nil
	}
	var onDelete []string
	if err := tx.Raw(`SELECT on_delete FROM pragma_foreign_key_list('claude_sessions') WHERE "table" = 'sessions'`).Scan(&onDelete).Error; err != nil {
		return err
	}
	if len(onDelete) == 1 && onDelete[0] == "CASCADE" {
		return nil
	}
	if mg.HasConstraint(&Session{}, "ClaudeSessions") {
		if err := mg.DropConstraint(&Session{}, "ClaudeSessions"); err != nil {
			return fmt.Errorf("drop claude_sessions foreign key: %w", err)
		}
	}
	if err := mg.CreateConstraint(&Session{}, "ClaudeSessions"); err != nil {
		return fmt.Errorf("create claude_sessions foreign key: %w", err)
	}
	return nil
}

func dropTmuxSessionNameColumn(tx *gorm.DB) error {
	mg := tx.Migrator()
	if err := db.DropIndexesIfExist(mg, "sessions", "idx_sessions_tmux_session_name"); err != nil {
		return err
	}
	if !mg.HasColumn("sessions", "tmux_session_name") {
		return nil
	}
	return tx.Exec("ALTER TABLE sessions DROP COLUMN tmux_session_name").Error
}

func renameSetupTmuxSteps(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("session_setup_steps") {
		return nil
	}
	if err := tx.Exec("UPDATE session_setup_steps SET kind = ? WHERE kind = ?", string(StepKindSetupMux), "setup_tmux").Error; err != nil {
		return err
	}
	return tx.Exec("UPDATE session_setup_steps SET label = ? WHERE kind = ? AND label = ?", setupMuxLabel, string(StepKindSetupMux), "Setup tmux").Error
}

func renameTmuxSessionColumn(tx *gorm.DB) error {
	mg := tx.Migrator()
	if err := db.DropConstraintIfExists(mg, "sessions", "fk_sessions_tmux_session"); err != nil {
		return err
	}
	if err := db.DropIndexesIfExist(mg, "sessions", "idx_sessions_tmux_session_id"); err != nil {
		return err
	}
	if !mg.HasColumn("sessions", "tmux_session_id") {
		return nil
	}
	if !mg.HasColumn("sessions", "mux_session_id") {
		return mg.RenameColumn("sessions", "tmux_session_id", "mux_session_id")
	}
	var linked int64
	if err := tx.Table("sessions").Unscoped().Where("tmux_session_id IS NOT NULL").Count(&linked).Error; err != nil {
		return err
	}
	if linked > 0 {
		return fmt.Errorf("sessions has both tmux_session_id and mux_session_id columns")
	}
	return tx.Exec("ALTER TABLE sessions DROP COLUMN tmux_session_id").Error
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

type prActivityTask struct {
	service *SessionService
}

func (t *prActivityTask) Name() string            { return "session.pr_activity" }
func (t *prActivityTask) Interval() time.Duration { return time.Minute }
func (t *prActivityTask) Run(ctx context.Context) error {
	return t.service.SyncPRActivity(ctx)
}

type sessionBranchesTask struct {
	service *SessionService
}

func (t *sessionBranchesTask) Name() string            { return "session.branches" }
func (t *sessionBranchesTask) Interval() time.Duration { return 2 * time.Minute }
func (t *sessionBranchesTask) Run(ctx context.Context) error {
	return t.service.SyncAllSessionBranches(ctx)
}

func (m *SessionModule) RegisterJobs(svc *jobs.JobService) {
	svc.Register(&reconcileSyncTask{service: m.Service})
	if !m.githubDisabled {
		svc.Register(&prActivityTask{service: m.Service})
	}
	svc.Register(&sessionBranchesTask{service: m.Service})
}
