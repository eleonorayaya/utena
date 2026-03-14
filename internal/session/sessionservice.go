package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type SessionService struct {
	store            *SessionStore
	workspaceService *workspace.WorkspaceService
	gitService       *git.GitService
	tmuxService      *utmux.TmuxService
	eventBus         eventbus.EventBus
	branchPrefix     string
}

func NewSessionService(store *SessionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, tmuxService *utmux.TmuxService, bus eventbus.EventBus, branchPrefix string) *SessionService {
	return &SessionService{
		store:            store,
		workspaceService: workspaceService,
		gitService:       gitService,
		tmuxService:      tmuxService,
		eventBus:         bus,
		branchPrefix:     branchPrefix,
	}
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.TmuxSessionCreated, s.handleTmuxSessionCreated)
	s.eventBus.Subscribe(eventbus.TmuxSessionClosed, s.handleTmuxSessionClosed)
	s.eventBus.Subscribe(eventbus.TmuxClientSessionChanged, s.handleTmuxClientSessionChanged)
	s.eventBus.Subscribe(eventbus.TmuxClientAttached, s.handleTmuxClientAttached)
	s.eventBus.Subscribe(eventbus.TmuxClientDetached, s.handleTmuxClientDetached)
	s.reconcileTmuxState(ctx)
	return nil
}

func (s *SessionService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *SessionService) ListSessions(ctx context.Context) ([]Session, error) {
	sessions := s.store.List()
	return sessions, nil
}

func (s *SessionService) ListSessionsByWorkspace(ctx context.Context, workspaceID uint) ([]Session, error) {
	_, err := s.workspaceService.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	return s.store.ListByWorkspace(workspaceID), nil
}

func (s *SessionService) GetSession(ctx context.Context, id uint) (*Session, error) {
	return s.store.GetByID(id)
}

func (s *SessionService) GetSessionByTmuxName(ctx context.Context, tmuxName string) (*Session, error) {
	return s.store.GetByTmuxName(tmuxName)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session, createWorktree bool) error {
	var ws *workspace.Workspace
	if session.WorkspaceID != 0 {
		var err error
		ws, err = s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	switch {
	case session.Name != "":
		if ws != nil {
			session.TmuxSessionName = BuildTmuxSessionName(ws.Name, session.Name)
		} else {
			session.TmuxSessionName = SanitizeTmuxName(session.Name)
		}
	case session.Branch != "":
		if session.Name == "" {
			session.Name = session.Branch
		}
		if ws != nil {
			session.TmuxSessionName = BuildTmuxSessionName(ws.Name, session.Name)
		} else {
			session.TmuxSessionName = SanitizeTmuxName(session.Name)
		}
	default:
		return fmt.Errorf("session name or branch is required")
	}

	res := &Resources{
		Tmux: &ResourceState{Status: ResourcePending},
	}
	if ws != nil && ws.IsGitRepo && createWorktree {
		res.Branch = &ResourceState{Status: ResourcePending}
		res.Worktree = &ResourceState{Status: ResourcePending}
	}
	session.Resources = res
	session.Status = StatusCreating

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	if session.WorkspaceID != 0 {
		s.workspaceService.Touch(ctx, session.WorkspaceID)
	}

	go s.runSetup(session.ID, ws)

	return nil
}

func (s *SessionService) runSetup(sessionID uint, ws *workspace.Workspace) {
	ctx := context.Background()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	if sess.Resources.Branch != nil && sess.Resources.Branch.Status != ResourceReady {
		sess.Resources.Branch.Status = ResourceCreating
		s.store.Update(sess)

		pullBranch := sess.BaseBranch
		if !sess.BranchCreated && sess.Branch != "" {
			pullBranch = sess.Branch
		}

		if pullBranch != "" {
			if err := s.gitService.Pull(ctx, ws.Path, pullBranch); err != nil {
				sess.Resources.Branch.Status = ResourceFailed
				sess.Resources.Branch.Error = fmt.Sprintf("failed to pull branch %q: %v", pullBranch, err)
				sess.Status = StatusBroken
				s.store.Update(sess)
				return
			}
		}

		sess.Resources.Branch.Status = ResourceReady
		s.store.Update(sess)
	}

	if sess.Resources.Worktree != nil && sess.Resources.Worktree.Status != ResourceReady {
		sess.Resources.Worktree.Status = ResourceCreating
		s.store.Update(sess)

		if sess.BranchCreated {
			branchName := s.branchPrefix + sess.Name
			worktreePath, err := s.gitService.CreateWorktree(ctx, ws.Path, branchName, sess.BaseBranch)
			if err != nil {
				sess.Resources.Worktree.Status = ResourceFailed
				sess.Resources.Worktree.Error = fmt.Sprintf("failed to create worktree: %v", err)
				sess.Status = StatusBroken
				s.store.Update(sess)
				return
			}
			sess.WorktreePath = worktreePath
			sess.Branch = branchName
		} else if sess.Branch != "" {
			worktreePath, err := s.gitService.CheckoutWorktree(ctx, ws.Path, sess.Branch)
			if err != nil {
				sess.Resources.Worktree.Status = ResourceFailed
				sess.Resources.Worktree.Error = fmt.Sprintf("failed to checkout worktree: %v", err)
				sess.Status = StatusBroken
				s.store.Update(sess)
				return
			}
			sess.WorktreePath = worktreePath
		}

		sess.Resources.Worktree.Status = ResourceReady
		s.store.Update(sess)
	}

	if sess.Resources.Tmux != nil && sess.Resources.Tmux.Status != ResourceReady {
		sess.Resources.Tmux.Status = ResourceCreating
		s.store.Update(sess)

		startDir := s.resolveStartDir(ctx, sess)
		if err := s.tmuxService.CreateSession(sess.TmuxSessionName, startDir); err != nil {
			sess.Resources.Tmux.Status = ResourceFailed
			sess.Resources.Tmux.Error = fmt.Sprintf("failed to create tmux session: %v", err)
			sess.Status = StatusBroken
			s.store.Update(sess)
			return
		}

		sess.Resources.Tmux.Status = ResourceReady
		s.store.Update(sess)
	}

	sess.Status = StatusReady
	s.store.Update(sess)
}

func (s *SessionService) RefreshSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}

	if sess.Resources == nil {
		return sess, nil
	}

	if sess.Resources.Worktree != nil && sess.Resources.Worktree.Status == ResourceReady {
		if sess.WorktreePath != "" {
			if _, err := os.Stat(sess.WorktreePath); os.IsNotExist(err) {
				sess.Resources.Worktree.Status = ResourceRemoved
			}
		}
	}

	if sess.Resources.Tmux != nil && sess.Resources.Tmux.Status == ResourceReady {
		if !s.tmuxService.HasSession(sess.TmuxSessionName) {
			sess.Resources.Tmux.Status = ResourceRemoved
		}
	}

	if sess.Status != StatusCreating && sess.Status != StatusDeleted {
		if sess.Resources.AllReady() {
			sess.Status = StatusReady
		} else {
			sess.Status = StatusBroken
		}
	}

	s.store.Update(sess)
	return sess, nil
}

func (s *SessionService) RepairSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.RefreshSession(ctx, id)
	if err != nil {
		return nil, err
	}

	if sess.Status == StatusReady {
		return sess, nil
	}

	if sess.Status != StatusBroken {
		return nil, ErrSessionNotBroken
	}

	if sess.Resources != nil {
		for _, rs := range []*ResourceState{sess.Resources.Branch, sess.Resources.Worktree, sess.Resources.Tmux} {
			if rs != nil && rs.Status != ResourceReady {
				rs.Status = ResourcePending
				rs.Error = ""
			}
		}
	}

	sess.Status = StatusCreating
	s.store.Update(sess)

	var ws *workspace.Workspace
	if sess.WorkspaceID != 0 {
		ws, _ = s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
	}

	go s.runSetup(sess.ID, ws)

	return sess, nil
}

func (s *SessionService) UpdateSession(ctx context.Context, session *Session) error {
	existing, err := s.store.GetByID(session.ID)
	if err != nil {
		return err
	}

	if session.WorkspaceID != 0 && session.WorkspaceID != existing.WorkspaceID {
		_, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	return s.store.Update(session)
}

func (s *SessionService) ActivateSession(ctx context.Context, id uint) (*Session, error) {
	session, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}

	slog.Info("activate session", "session", id, "status", session.Status, "is_attached", session.IsAttached)

	if session.Status == StatusCreating || session.Status == StatusBroken {
		return nil, ErrCannotActivate
	}

	if !s.tmuxService.HasSession(session.TmuxSessionName) {
		startDir := s.resolveStartDir(ctx, session)
		if err := s.tmuxService.CreateSession(session.TmuxSessionName, startDir); err != nil {
			return nil, fmt.Errorf("failed to revive tmux session: %w", err)
		}
		session.Status = StatusReady
		if session.Resources != nil && session.Resources.Tmux != nil {
			session.Resources.Tmux.Status = ResourceReady
			session.Resources.Tmux.Error = ""
		}
	}

	if session.IsAttached {
		slog.Info("skipping activation for already attached session", "session", id)
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
	session.IsAttached = true

	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	if session.WorkspaceID != 0 {
		s.workspaceService.Touch(ctx, session.WorkspaceID)
	}

	s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: session.TmuxSessionName},
	})

	return session, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id uint, deleteBranch bool) error {
	session, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	if session.IsAttached {
		return ErrSessionAttached
	}

	if session.Status == StatusCreating {
		return fmt.Errorf("cannot delete session while it is being created")
	}

	if session.Resources == nil {
		session.Resources = &Resources{}
	}

	if session.WorkspaceID != 0 {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			if session.WorktreePath != "" {
				if rmErr := s.gitService.RemoveWorktree(ctx, ws.Path, session.WorktreePath); rmErr != nil {
					slog.Warn("failed to remove worktree", "error", rmErr)
				} else if session.Resources.Worktree != nil {
					session.Resources.Worktree.Status = ResourceRemoved
				}
			}
			if deleteBranch && session.Branch != "" {
				if brErr := s.gitService.DeleteBranch(ctx, ws.Path, session.Branch); brErr != nil {
					slog.Warn("failed to delete branch", "error", brErr)
				} else if session.Resources.Branch != nil {
					session.Resources.Branch.Status = ResourceRemoved
				}
			}
		} else {
			slog.Warn("workspace not found during cleanup, skipping worktree/branch removal", "workspace_id", session.WorkspaceID)
		}
	}

	if err := s.tmuxService.KillSession(session.TmuxSessionName); err != nil {
		slog.Info("tmux session already gone or failed to kill", "session", session.TmuxSessionName, "error", err)
	} else if session.Resources.Tmux != nil {
		session.Resources.Tmux.Status = ResourceRemoved
	}

	session.Status = StatusDeleted

	return s.store.Update(session)
}

func (s *SessionService) resolveStartDir(ctx context.Context, session *Session) string {
	if session.WorktreePath != "" {
		return session.WorktreePath
	}
	if session.WorkspaceID != 0 {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			return ws.Path
		}
	}
	return ""
}

func (s *SessionService) handleTmuxSessionCreated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.store.GetByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for session-created hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	s.RefreshSession(ctx, sess.ID)
	return nil
}

func (s *SessionService) handleTmuxSessionClosed(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.store.GetByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for session-closed hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	sess.IsAttached = false
	if sess.Resources != nil && sess.Resources.Tmux != nil {
		sess.Resources.Tmux.Status = ResourceRemoved
	}
	sess.Status = StatusBroken
	return s.store.Update(sess)
}

func (s *SessionService) handleTmuxClientSessionChanged(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sessions := s.store.List()
	for _, sess := range sessions {
		if sess.IsAttached {
			sess.IsAttached = false
			if err := s.store.Update(&sess); err != nil {
				return err
			}
		}
	}

	sess, err := s.store.GetByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for client-session-changed hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	return s.store.Update(sess)
}

func (s *SessionService) handleTmuxClientAttached(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.store.GetByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for client-attached hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	return s.store.Update(sess)
}

func (s *SessionService) handleTmuxClientDetached(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.TmuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.store.GetByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for client-detached hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	sess.IsAttached = false
	return s.store.Update(sess)
}

func (s *SessionService) reconcileTmuxState(ctx context.Context) {
	for _, sess := range s.store.List() {
		if sess.Status != StatusDeleted {
			s.RefreshSession(ctx, sess.ID)
		}
	}
}
