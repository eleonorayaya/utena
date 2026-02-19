package session

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const EventSessionDeleted = "session.deleted"

type SessionService struct {
	store            *SessionStore
	workspaceService *workspace.WorkspaceService
	gitService       *git.GitService
	eventBus         eventbus.EventBus
}

func NewSessionService(store *SessionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, bus eventbus.EventBus) *SessionService {
	return &SessionService{
		store:            store,
		workspaceService: workspaceService,
		gitService:       gitService,
		eventBus:         bus,
	}
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	return nil
}

func (s *SessionService) OnAppEnd(ctx context.Context) error {

	return nil
}

func (s *SessionService) ListSessions(ctx context.Context) ([]Session, error) {
	sessions := s.store.List()
	for i := range sessions {
		s.resolveWorkspaceName(&sessions[i])
	}
	return sessions, nil
}

func (s *SessionService) resolveWorkspaceName(session *Session) {
	if session.WorkspaceID == "" {
		return
	}
	ws, err := s.workspaceService.GetWorkspace(context.Background(), session.WorkspaceID)
	if err != nil {
		slog.Warn("failed to resolve workspace name", "session", session.ID, "workspace_id", session.WorkspaceID, "error", err)
		return
	}
	session.WorkspaceName = ws.Name
}

func (s *SessionService) ListSessionsByWorkspace(ctx context.Context, workspaceID string) ([]Session, error) {

	_, err := s.workspaceService.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	return s.store.ListByWorkspace(workspaceID), nil
}

func (s *SessionService) GetSession(ctx context.Context, id string) (*Session, error) {
	return s.store.GetByID(id)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session) error {
	var ws *workspace.Workspace
	if session.WorkspaceID != "" {
		var err error
		ws, err = s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	if session.BaseBranch != "" && ws != nil && ws.IsGitRepo {
		if err := s.gitService.Pull(ctx, ws.Path, session.BaseBranch); err != nil {
			slog.Warn("git pull failed, continuing with worktree creation", "error", err)
		}

		worktreePath, err := s.gitService.CreateWorktree(ctx, ws.Path, session.ID, session.BaseBranch)
		if err != nil {
			return fmt.Errorf("failed to create worktree: %w", err)
		}
		session.WorktreePath = worktreePath
	}

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	return nil
}

func (s *SessionService) UpdateSession(ctx context.Context, session *Session) error {
	existing, err := s.store.GetByID(session.ID)
	if err != nil {
		return err
	}

	if session.WorkspaceID != "" && session.WorkspaceID != existing.WorkspaceID {
		_, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	return s.store.Update(session)
}

func (s *SessionService) ActivateSession(ctx context.Context, name string) (*Session, error) {
	session, err := s.store.GetByID(name)
	if err != nil {
		return nil, err
	}

	slog.Info("activate session", "session", name, "is_attached", session.IsAttached)

	if session.IsAttached {
		slog.Info("skipping activation for already attached session", "session", name)
		return session, nil
	}

	for _, existing := range s.store.List() {
		if existing.IsAttached {
			slog.Info("clearing attached flag", "session", existing.ID)
			existing.IsAttached = false
			s.store.Update(&existing)
		}
	}

	session.LastUsedAt = time.Now()
	session.IsActive = true
	session.IsAttached = true

	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	return session, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id string) error {
	session, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	if session.IsAttached {
		return ErrSessionAttached
	}

	cleanup := &Cleanup{}

	if session.WorktreePath != "" && session.WorkspaceID != "" {
		ws, err := s.workspaceStore.GetByID(session.WorkspaceID)
		if err == nil {
			if rmErr := s.gitService.RemoveWorktree(ctx, ws.Path, session.WorktreePath); rmErr != nil {
				slog.Warn("failed to remove worktree", "error", rmErr)
			} else {
				cleanup.WorktreeRemoved = true
			}
			if brErr := s.gitService.DeleteBranch(ctx, ws.Path, id); brErr != nil {
				slog.Warn("failed to delete branch", "error", brErr)
			} else {
				cleanup.BranchDeleted = true
			}
		} else {
			slog.Warn("workspace not found during cleanup, skipping worktree/branch removal", "workspace_id", session.WorkspaceID)
		}
	}

	session.IsDeleted = true
	session.Cleanup = cleanup

	if err := s.store.Update(session); err != nil {
		return err
	}

	return s.eventBus.Publish(ctx, eventbus.Event{
		Type: EventSessionDeleted,
		Data: session,
	})
}

