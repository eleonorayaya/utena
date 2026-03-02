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
	branchPrefix     string
}

func NewSessionService(store *SessionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, bus eventbus.EventBus, branchPrefix string) *SessionService {
	return &SessionService{
		store:            store,
		workspaceService: workspaceService,
		gitService:       gitService,
		eventBus:         bus,
		branchPrefix:     branchPrefix,
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

func (s *SessionService) GetSessionByTmuxName(ctx context.Context, tmuxName string) (*Session, error) {
	return s.store.GetByTmuxName(tmuxName)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session, createWorktree bool) error {
	var ws *workspace.Workspace
	if session.WorkspaceID != "" {
		var err error
		ws, err = s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	switch {
	case session.Name != "":
		if ws != nil {
			session.ID = BuildSessionID(ws.Name, session.Name)
		} else {
			session.ID = session.Name
		}
	case session.Branch != "":
		session.Name = sanitizeBranchName(session.Branch)
		if ws != nil {
			session.ID = BuildSessionID(ws.Name, session.Name)
		} else {
			session.ID = session.Name
		}
	default:
		return fmt.Errorf("session name or branch is required")
	}

	session.TmuxSessionName = SanitizeForTmux(session.ID)

	if ws != nil && ws.IsGitRepo && createWorktree {
		pullBranch := session.BaseBranch
		if !session.BranchCreated && session.Branch != "" {
			pullBranch = session.Branch
		}

		if pullBranch != "" {
			if err := s.gitService.Pull(ctx, ws.Path, pullBranch); err != nil {
				return fmt.Errorf("failed to pull branch %q: %w", pullBranch, err)
			}
		}

		if session.BranchCreated {
			branchName := s.branchPrefix + session.Name
			worktreePath, err := s.gitService.CreateWorktree(ctx, ws.Path, session.Name, branchName, session.BaseBranch)
			if err != nil {
				return fmt.Errorf("failed to create worktree: %w", err)
			}
			session.WorktreePath = worktreePath
			session.BranchName = branchName
			session.Branch = branchName
		} else if session.Branch != "" {
			worktreePath, err := s.gitService.CheckoutWorktree(ctx, ws.Path, session.Branch)
			if err != nil {
				return fmt.Errorf("failed to checkout worktree: %w", err)
			}
			session.WorktreePath = worktreePath
		}
	}

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	if session.WorkspaceID != "" {
		s.workspaceService.Touch(ctx, session.WorkspaceID)
	}

	startDir := ""
	if session.WorktreePath != "" {
		startDir = session.WorktreePath
	} else if session.WorkspaceID != "" {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			startDir = ws.Path
		}
	}
	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionCreateRequested,
		Data: eventbus.SessionCreateRequestedEvent{
			SessionName:   session.ID,
			WorkspacePath: startDir,
		},
	})

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

	if session.IsDead {
		return nil, ErrSessionDead
	}

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

	if session.WorkspaceID != "" {
		s.workspaceService.Touch(ctx, session.WorkspaceID)
	}

	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: name},
	})

	return session, nil
}

func (s *SessionService) ReviveSession(ctx context.Context, name string) (*ReviveResult, error) {
	session, err := s.store.GetByID(name)
	if err != nil {
		return nil, err
	}

	if !session.IsDead {
		return nil, ErrSessionNotDead
	}

	var workspacePath string
	if session.WorkspaceID != "" {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return nil, err
		}
		workspacePath = ws.Path
	}

	session.IsDead = false
	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	startDir := workspacePath
	if session.WorktreePath != "" {
		startDir = session.WorktreePath
	}
	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionRevived,
		Data: eventbus.SessionRevivedEvent{
			SessionName:   session.ID,
			WorkspacePath: startDir,
		},
	})

	return &ReviveResult{Session: session, WorkspacePath: workspacePath}, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id string, deleteBranch bool) error {
	session, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	if session.IsAttached {
		return ErrSessionAttached
	}

	cleanup := &Cleanup{}

	if session.WorkspaceID != "" {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			if session.WorktreePath != "" {
				if rmErr := s.gitService.RemoveWorktree(ctx, ws.Path, session.WorktreePath); rmErr != nil {
					slog.Warn("failed to remove worktree", "error", rmErr)
				} else {
					cleanup.WorktreeRemoved = true
				}
			}
			if deleteBranch {
				branchToDelete := session.BranchName
				if branchToDelete == "" {
					branchToDelete = session.Branch
				}
				if branchToDelete != "" {
					if brErr := s.gitService.DeleteBranch(ctx, ws.Path, branchToDelete); brErr != nil {
						slog.Warn("failed to delete branch", "error", brErr)
					} else {
						cleanup.BranchDeleted = true
					}
				}
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
