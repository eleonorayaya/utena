package session

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type SessionService struct {
	store            *SessionStore
	dismissedPRStore *DismissedPRStore
	workspaceService *workspace.WorkspaceService
	gitService       *git.GitService
	tmuxService      *utmux.TmuxService
	eventBus         eventbus.EventBus
	branchPrefix     string
	configDir        string
}

func NewSessionService(store *SessionStore, dismissedPRStore *DismissedPRStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, tmuxService *utmux.TmuxService, bus eventbus.EventBus, branchPrefix string, configDir string) *SessionService {
	return &SessionService{
		store:            store,
		dismissedPRStore: dismissedPRStore,
		workspaceService: workspaceService,
		gitService:       gitService,
		tmuxService:      tmuxService,
		eventBus:         bus,
		branchPrefix:     branchPrefix,
		configDir:        configDir,
	}
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.TmuxSessionCreated, s.handleTmuxSessionCreated)
	s.eventBus.Subscribe(eventbus.TmuxSessionClosed, s.handleTmuxSessionClosed)
	s.eventBus.Subscribe(eventbus.TmuxClientSessionChanged, s.handleTmuxClientSessionChanged)
	s.eventBus.Subscribe(eventbus.TmuxClientAttached, s.handleTmuxClientAttached)
	s.eventBus.Subscribe(eventbus.TmuxClientDetached, s.handleTmuxClientDetached)
	s.eventBus.Subscribe(git.EventPRDiscovered, s.handlePRDiscovered)
	s.eventBus.Subscribe(git.EventPRStateChanged, s.handlePRStateChanged)
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

func (s *SessionService) findSessionByTmuxName(tmuxName string) (*Session, error) {
	ts, err := s.tmuxService.GetSessionByName(tmuxName)
	if err != nil {
		return nil, err
	}
	return s.store.GetByTmuxSessionID(ts.ID)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session, branchName string, baseBranchName string, createWorktree bool) error {
	var ws *workspace.Workspace
	if session.WorkspaceID != 0 {
		var err error
		ws, err = s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err != nil {
			return err
		}
	}

	if session.TodoID != nil && ws != nil && ws.IsGitRepo && baseBranchName == "" {
		baseBranchName = "main"
		createWorktree = true
	}

	var tmuxName string
	switch {
	case session.Name != "":
		if ws != nil {
			tmuxName = BuildTmuxSessionName(ws.Name, session.Name)
		} else {
			tmuxName = SanitizeTmuxName(session.Name)
		}
	case branchName != "":
		if session.Name == "" {
			session.Name = branchName
		}
		if ws != nil {
			tmuxName = BuildTmuxSessionName(ws.Name, session.Name)
		} else {
			tmuxName = SanitizeTmuxName(session.Name)
		}
	default:
		return fmt.Errorf("session name or branch is required")
	}

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

	go s.runSetup(session.ID, ws, tmuxName, branchName, baseBranchName)

	return nil
}

func (s *SessionService) runSetup(sessionID uint, ws *workspace.Workspace, tmuxName string, branchName string, baseBranchName string) {
	ctx := context.Background()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	var worktreeCreated bool
	var worktreePath string

	needsGitSetup := ws != nil && ws.IsGitRepo && (branchName != "" || baseBranchName != "")
	if needsGitSetup {
		if err := s.setupBranch(ctx, ws, branchName, baseBranchName); err != nil {
			sess.Status = StatusBroken
			sess.StatusError = fmt.Sprintf("branch setup failed: %v", err)
			s.store.Update(sess)
			return
		}

		created, wtPath, err := s.setupWorktree(ctx, sess, ws, branchName, baseBranchName)
		if err != nil {
			sess.Status = StatusBroken
			sess.StatusError = fmt.Sprintf("worktree setup failed: %v", err)
			s.store.Update(sess)
			return
		}
		worktreeCreated = created
		worktreePath = wtPath

		if err := s.setupWorktreeInit(ctx, sess, ws, worktreeCreated, worktreePath, branchName, baseBranchName); err != nil {
			slog.Warn("worktree init failed, continuing", "error", err)
		}
	}

	if err := s.setupTmux(ctx, sess, tmuxName); err != nil {
		sess.Status = StatusBroken
		sess.StatusError = fmt.Sprintf("tmux setup failed: %v", err)
		s.store.Update(sess)
		return
	}

	sess.Status = StatusActive
	sess.StatusError = ""
	s.store.Update(sess)
}

func (s *SessionService) setupBranch(ctx context.Context, ws *workspace.Workspace, branchName string, baseBranchName string) error {
	pullBranch := branchName
	if baseBranchName != "" {
		pullBranch = baseBranchName
	}

	hasRemote, err := s.gitService.HasRemoteBranch(ctx, ws.Path, pullBranch)
	if err != nil {
		return fmt.Errorf("failed to check remote branch %q: %v", pullBranch, err)
	}

	if !hasRemote {
		hasLocal, err := s.gitService.HasBranch(ctx, ws.Path, pullBranch)
		if err != nil {
			return fmt.Errorf("failed to check local branch %q: %v", pullBranch, err)
		}
		if !hasLocal {
			return fmt.Errorf("branch %q does not exist locally or on remote", pullBranch)
		}
		return nil
	}

	checkPath := s.gitService.WorktreePath(ws.Path, pullBranch)
	if _, err := os.Stat(checkPath); os.IsNotExist(err) {
		checkPath = ws.Path
	}

	dirty, err := s.gitService.IsDirty(ctx, checkPath)
	if err != nil {
		return fmt.Errorf("failed to check dirty state for %q: %v", pullBranch, err)
	}

	if dirty {
		return nil
	}

	if err := s.gitService.Pull(ctx, ws.Path, pullBranch); err != nil {
		return fmt.Errorf("failed to pull branch %q: %v", pullBranch, err)
	}

	return nil
}

func (s *SessionService) setupWorktree(ctx context.Context, sess *Session, ws *workspace.Workspace, branchName string, baseBranchName string) (bool, string, error) {
	creatingNewBranch := baseBranchName != ""

	finalBranchName := branchName
	if creatingNewBranch {
		finalBranchName = s.branchPrefix + sess.Name
	}

	branchExists, err := s.gitService.HasBranch(ctx, ws.Path, finalBranchName)
	if err != nil {
		return false, "", fmt.Errorf("failed to check branch %q: %v", finalBranchName, err)
	}
	if creatingNewBranch && branchExists {
		return false, "", fmt.Errorf("branch %q already exists; use it as an existing branch instead", finalBranchName)
	}
	if !creatingNewBranch && !branchExists {
		return false, "", fmt.Errorf("branch %q does not exist; provide a base branch to create it", finalBranchName)
	}

	worktreePath := s.gitService.WorktreePath(ws.Path, finalBranchName)

	exists, err := s.gitService.ValidateWorktree(ctx, worktreePath, finalBranchName)
	if err != nil {
		return false, "", err
	}
	if exists {
		return false, worktreePath, nil
	}

	if creatingNewBranch {
		path, err := s.gitService.CreateWorktree(ctx, ws.Path, finalBranchName, baseBranchName)
		if err != nil {
			return false, "", fmt.Errorf("failed to create worktree: %v", err)
		}
		return true, path, nil
	}
	path, err := s.gitService.CheckoutWorktree(ctx, ws.Path, finalBranchName)
	if err != nil {
		return false, "", fmt.Errorf("failed to checkout worktree: %v", err)
	}
	return true, path, nil
}

func (s *SessionService) setupTmux(ctx context.Context, sess *Session, tmuxName string) error {
	startDir := s.resolveStartDir(ctx, sess)
	env := map[string]string{"UTENA_SESSION_ID": fmt.Sprintf("%d", sess.ID)}
	ts, err := s.tmuxService.CreateSession(tmuxName, startDir, env)
	if err != nil {
		return fmt.Errorf("failed to create tmux session: %v", err)
	}
	sess.TmuxSessionID = &ts.ID
	return nil
}

func (s *SessionService) setupWorktreeInit(ctx context.Context, sess *Session, ws *workspace.Workspace, worktreeCreated bool, worktreePath string, branchName string, baseBranchName string) error {
	if !worktreeCreated {
		return nil
	}

	finalBranchName := branchName
	if baseBranchName != "" {
		finalBranchName = s.branchPrefix + sess.Name
	}

	env := []string{
		"UTENA_WORKTREE_PATH=" + worktreePath,
		"UTENA_BRANCH=" + finalBranchName,
		"UTENA_SESSION_NAME=" + sess.Name,
	}
	if ws != nil {
		env = append(env,
			"UTENA_WORKSPACE_NAME="+ws.Name,
			"UTENA_WORKSPACE_PATH="+ws.Path,
		)
	}

	scripts := []string{
		filepath.Join(s.configDir, "worktree-setup"),
	}
	if ws != nil {
		scripts = append(scripts, filepath.Join(ws.Path, ".utena", "worktree-setup"))
	}

	var warnings []string
	for _, script := range scripts {
		if err := s.runScript(ctx, script, worktreePath, env); err != nil {
			slog.Warn("worktree setup script failed", "script", script, "error", err)
			warnings = append(warnings, err.Error())
		}
	}

	if len(warnings) > 0 {
		return fmt.Errorf("%s", strings.Join(warnings, "; "))
	}
	return nil
}

func (s *SessionService) runScript(ctx context.Context, path string, workDir string, env []string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat script %s: %w", path, err)
	}

	if info.Mode()&0111 == 0 {
		return fmt.Errorf("script %s exists but is not executable", path)
	}

	cmd := exec.CommandContext(ctx, path)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("script %s failed: %s: %w", path, strings.TrimSpace(string(output)), err)
	}
	return nil
}

func (s *SessionService) RefreshSession(ctx context.Context, id uint) (*Session, error) {
	return s.ReconcileSession(ctx, id)
}

func (s *SessionService) RepairSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status != StatusBroken {
		return nil, ErrSessionNotBroken
	}

	sess.Status = StatusCreating
	sess.StatusError = ""
	s.store.Update(sess)

	var ws *workspace.Workspace
	if sess.WorkspaceID != 0 {
		ws, _ = s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
	}

	branchName := ""
	baseBranchName := ""
	if sess.GitBranch != nil {
		branchName = sess.GitBranch.Name
		if sess.GitBranch.BaseBranch != nil {
			baseBranchName = sess.GitBranch.BaseBranch.Name
		}
	}

	tmuxName := s.computeTmuxName(sess, ws)
	go s.runSetup(sess.ID, ws, tmuxName, branchName, baseBranchName)
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

	tmuxName := ""
	if session.TmuxSessionID != nil {
		ts, tsErr := s.tmuxService.GetSession(*session.TmuxSessionID)
		if tsErr == nil {
			tmuxName = ts.Name
		}
	}
	if tmuxName == "" {
		var ws *workspace.Workspace
		if session.WorkspaceID != 0 {
			ws, _ = s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		}
		tmuxName = s.computeTmuxName(session, ws)
	}

	if !s.tmuxService.HasSession(tmuxName) {
		startDir := s.resolveStartDir(ctx, session)
		env := map[string]string{"UTENA_SESSION_ID": fmt.Sprintf("%d", session.ID)}
		ts, createErr := s.tmuxService.CreateSession(tmuxName, startDir, env)
		if createErr != nil {
			return nil, fmt.Errorf("failed to revive tmux session: %w", createErr)
		}
		session.TmuxSessionID = &ts.ID
		session.Status = StatusActive
		session.StatusError = ""
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
		Data: eventbus.SessionActivatedEvent{SessionName: tmuxName},
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

	if session.BranchID != nil && session.GitBranch != nil && session.WorkspaceID != 0 {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			if err := s.gitService.CleanupBranch(ctx, session.GitBranch, ws.Path, deleteBranch); err != nil {
				slog.Warn("failed to cleanup branch", "error", err)
			}
		} else {
			slog.Warn("workspace not found during cleanup, skipping worktree/branch removal", "workspace_id", session.WorkspaceID)
		}
	}

	if session.TmuxSessionID != nil {
		if err := s.tmuxService.KillSession(*session.TmuxSessionID); err != nil {
			slog.Info("failed to kill tmux session", "error", err)
		}
	}

	session.Status = StatusDeleted

	return s.store.Update(session)
}

func (s *SessionService) computeTmuxName(sess *Session, ws *workspace.Workspace) string {
	if ws != nil {
		return BuildTmuxSessionName(ws.Name, sess.Name)
	}
	return SanitizeTmuxName(sess.Name)
}

func (s *SessionService) resolveStartDir(ctx context.Context, session *Session) string {
	if session.BranchID != nil && session.GitBranch != nil {
		ws, _ := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if ws != nil {
			return s.gitService.GetStartDir(session.GitBranch, ws.Path)
		}
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

	sess, err := s.findSessionByTmuxName(data.TmuxSessionName)
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

	sess, err := s.findSessionByTmuxName(data.TmuxSessionName)
	if err != nil {
		slog.Debug("session not found for session-closed hook", "tmux_name", data.TmuxSessionName, "error", err)
		return nil
	}

	sess.IsAttached = false
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

	sess, err := s.findSessionByTmuxName(data.TmuxSessionName)
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

	sess, err := s.findSessionByTmuxName(data.TmuxSessionName)
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

	sess, err := s.findSessionByTmuxName(data.TmuxSessionName)
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

func (s *SessionService) ArchiveSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status != StatusActive && sess.Status != StatusInactive {
		return nil, fmt.Errorf("cannot archive session in status %s", sess.Status)
	}

	if sess.BranchID != nil && sess.GitBranch != nil {
		ws, _ := s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
		if ws != nil {
			s.gitService.CleanupBranch(ctx, sess.GitBranch, ws.Path, false)
		}
	}

	if sess.TmuxSessionID != nil {
		s.tmuxService.KillSession(*sess.TmuxSessionID)
	}

	sess.Status = StatusArchived
	sess.IsAttached = false
	if err := s.store.Update(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *SessionService) DismissSession(ctx context.Context, id uint) error {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return err
	}
	if sess.Status != StatusPending {
		return fmt.Errorf("can only dismiss pending sessions")
	}

	if sess.BranchID != nil && s.dismissedPRStore != nil {
		prs := s.gitService.GetPRsForBranch(*sess.BranchID)
		for _, pr := range prs {
			s.dismissedPRStore.Add(&DismissedPR{
				PullRequestID: pr.ID,
				DismissedAt:   time.Now(),
			})
		}
	}

	return s.store.Delete(sess.ID)
}

func (s *SessionService) ReconcileSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}

	if sess.Status == StatusCreating || sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusPending {
		return sess, nil
	}

	tmuxAlive := false
	if sess.TmuxSessionID != nil {
		ts, tsErr := s.tmuxService.GetSession(*sess.TmuxSessionID)
		if tsErr == nil {
			tmuxAlive = ts.IsAlive
		}
	}

	gitHealthy := true
	if sess.BranchID != nil && sess.GitBranch != nil {
		ws, _ := s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
		if ws != nil {
			gitHealthy = s.gitService.IsHealthy(ctx, sess.GitBranch, ws.Path)
		}
	}

	if !gitHealthy {
		sess.Status = StatusBroken
		sess.StatusError = "worktree or branch is unhealthy"
	} else if !tmuxAlive {
		if sess.Status == StatusActive {
			sess.Status = StatusInactive
		}
	} else {
		if sess.Status == StatusInactive {
			sess.Status = StatusActive
		}
	}

	s.store.Update(sess)
	return sess, nil
}

func (s *SessionService) handlePRDiscovered(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(git.PRDiscoveredEvent)
	if !ok {
		return nil
	}

	if data.PullRequest.HeadBranchID == nil {
		return nil
	}

	_, err := s.store.GetByBranchID(*data.PullRequest.HeadBranchID)
	if err == nil {
		return nil
	}

	if s.dismissedPRStore != nil && s.dismissedPRStore.IsDismissed(data.PullRequest.ID) {
		return nil
	}

	slog.Info("PR discovered, could create pending session", "pr", data.PullRequest.Number, "branch", data.PullRequest.HeadBranchID)
	return nil
}

func (s *SessionService) handlePRStateChanged(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(git.PRStateChangedEvent)
	if !ok {
		return nil
	}

	if data.NewState == git.PRStateMerged && data.PullRequest.HeadBranchID != nil {
		slog.Info("PR merged", "pr", data.PullRequest.Number, "branch_id", *data.PullRequest.HeadBranchID)
	}
	return nil
}
