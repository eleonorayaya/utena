package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	utmux "github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/workspace"
	slogctx "github.com/veqryn/slog-context"
)

type SetupWarning struct{ Message string }

func (w SetupWarning) Error() string { return w.Message }

const (
	envSessionID      = "UTENA_SESSION_ID"
	envSessionName    = "UTENA_SESSION_NAME"
	envSessionRoot    = "UTENA_SESSION_ROOT"
	envBranch         = "UTENA_BRANCH"
	envWorktreePath   = "UTENA_WORKTREE_PATH"
	envWorkspaceName  = "UTENA_WORKSPACE_NAME"
	envWorkspacePath  = "UTENA_WORKSPACE_PATH"
	worktreeSetupName = "worktree-setup"
)

const defaultSetupTimeout = 5 * time.Minute

func traceOp(ctx context.Context, op string, fn func() error, attrs ...any) error {
	opAttrs := append([]any{"op", op}, attrs...)
	slog.InfoContext(ctx, "setup op: start", opAttrs...)
	start := time.Now()
	err := fn()
	doneAttrs := append([]any{"op", op, "duration_ms", time.Since(start).Milliseconds()}, attrs...)
	if err != nil {
		doneAttrs = append(doneAttrs, "error", err.Error())
	}
	slog.InfoContext(ctx, "setup op: done", doneAttrs...)
	return err
}

func tracedOp[T any](ctx context.Context, op string, fn func() (T, error), attrs ...any) (T, error) {
	var v T
	err := traceOp(ctx, op, func() error {
		var e error
		v, e = fn()
		return e
	}, attrs...)
	return v, err
}

type SessionService struct {
	store                 *SessionStore
	sessionWorkspaceStore *SessionWorkspaceStore
	dismissedPRStore      *DismissedPRStore
	sessionActionStore    *SessionActionStore
	workspaceService      *workspace.WorkspaceService
	gitService            *git.GitService
	tmuxService           *utmux.TmuxService
	eventBus              eventbus.EventBus
	branchPrefix          string
	configDir             string
	sessionsRoot          string
	setupTimeout          time.Duration
}

func NewSessionService(store *SessionStore, sessionWorkspaceStore *SessionWorkspaceStore, dismissedPRStore *DismissedPRStore, sessionActionStore *SessionActionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, tmuxService *utmux.TmuxService, bus eventbus.EventBus, branchPrefix string, configDir string, sessionsRoot string) *SessionService {
	return &SessionService{
		store:                 store,
		sessionWorkspaceStore: sessionWorkspaceStore,
		dismissedPRStore:      dismissedPRStore,
		sessionActionStore:    sessionActionStore,
		workspaceService:      workspaceService,
		gitService:            gitService,
		tmuxService:           tmuxService,
		eventBus:              bus,
		branchPrefix:          branchPrefix,
		configDir:             configDir,
		sessionsRoot:          sessionsRoot,
		setupTimeout:          defaultSetupTimeout,
	}
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.TmuxSessionCreated, s.handleTmuxSessionCreated)
	s.eventBus.Subscribe(eventbus.TmuxSessionClosed, s.handleTmuxSessionClosed)
	s.eventBus.Subscribe(eventbus.TmuxClientSessionChanged, s.handleTmuxClientSessionChanged)
	s.eventBus.Subscribe(eventbus.TmuxClientAttached, s.handleTmuxClientAttached)
	s.eventBus.Subscribe(eventbus.TmuxClientDetached, s.handleTmuxClientDetached)
	s.eventBus.Subscribe(git.EventPRUpdated, s.handlePRUpdated)
	s.backfillSessionWorkspaces(ctx)
	s.recoverStuckCreatingSessions()
	s.reconcileTmuxState(ctx)
	return nil
}

func (s *SessionService) backfillSessionWorkspaces(ctx context.Context) {
	if !s.store.hasPendingBackfill() {
		return
	}
	sessions, err := s.store.List()
	if err != nil {
		slog.Warn("backfill: failed to list sessions", "error", err)
		return
	}
	for i := range sessions {
		sess := &sessions[i]
		if len(sess.Workspaces) > 0 || sess.WorkspaceID == 0 {
			continue
		}
		ws, err := s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
		if err != nil {
			slog.Warn("backfill: workspace not found", "session", sess.ID, "workspace", sess.WorkspaceID, "error", err)
			continue
		}
		wtPath := ws.Path
		if sess.BranchID != nil && sess.GitBranch != nil {
			wtPath = s.gitService.GetStartDir(sess.GitBranch, ws.Path)
		}
		sw := &SessionWorkspace{
			SessionID:    sess.ID,
			WorkspaceID:  ws.ID,
			BranchID:     sess.BranchID,
			WorktreePath: wtPath,
			Position:     0,
		}
		if err := s.sessionWorkspaceStore.Add(sw); err != nil {
			slog.Warn("backfill: failed to add session workspace junction", "session", sess.ID, "error", err)
			continue
		}
		if sess.SessionRoot == "" {
			sess.SessionRoot = wtPath
			if err := s.store.Update(sess); err != nil {
				slog.Warn("backfill: failed to persist session root", "session", sess.ID, "error", err)
			}
		}
	}
}

func (s *SessionService) recoverStuckCreatingSessions() {
	sessions, err := s.store.List()
	if err != nil {
		slog.Warn("failed to list sessions for stuck-creating recovery", "error", err)
		return
	}
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status != StatusCreating {
			continue
		}
		if err := s.markSessionBroken(sess, "creation interrupted by daemon restart"); err != nil {
			slog.Warn("failed to recover stuck creating session", "session", sess.ID, "error", err)
			continue
		}
		slog.Info("recovered stuck creating session", "session", sess.ID, "name", sess.Name)
	}
}

func (s *SessionService) OnAppEnd(ctx context.Context) error {
	return nil
}

func (s *SessionService) populateTmuxWindows(ctx context.Context, sess *Session) {
	if sess.TmuxSession != nil {
		sess.TmuxSession.Windows = s.tmuxService.GetWindows(ctx, sess.TmuxSession.Name)
	}
}

func (s *SessionService) ListSessions(ctx context.Context) ([]Session, error) {
	sessions, err := s.store.List()
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		s.populateTmuxWindows(ctx, &sessions[i])
	}
	return sessions, nil
}

func (s *SessionService) ListSessionsByWorkspace(ctx context.Context, workspaceID uint) ([]Session, error) {
	_, err := s.workspaceService.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	sessions, err := s.store.ListByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		s.populateTmuxWindows(ctx, &sessions[i])
	}
	return sessions, nil
}

func (s *SessionService) GetSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	s.populateTmuxWindows(ctx, sess)
	return sess, nil
}

func (s *SessionService) findSessionByTmuxName(tmuxName string) (*Session, error) {
	ts, err := s.tmuxService.GetSessionByName(tmuxName)
	if err != nil {
		return nil, err
	}
	return s.store.GetByTmuxSessionID(ts.ID)
}

func (s *SessionService) CreateSession(ctx context.Context, session *Session, branchName string, baseBranchName string, createWorktree bool, actions ...*SessionAction) error {
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

	if ws != nil && ws.IsBare {
		createWorktree = true
	}

	if ws != nil && ws.IsGitRepo && branchName != "" && baseBranchName == "" && branchName != "main" {
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
			session.Name = s.nameFromBranch(branchName)
		}
		if ws != nil {
			tmuxName = BuildTmuxSessionName(ws.Name, session.Name)
		} else {
			tmuxName = SanitizeTmuxName(session.Name)
		}
	default:
		return common.NewInvalidRequest("session name or branch is required")
	}

	session.Status = StatusCreating

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	if session.WorkspaceID != 0 && session.Name != "" {
		_, err := s.store.GetByWorkspaceAndName(session.WorkspaceID, session.Name, StatusDeleted, StatusArchived)
		switch {
		case err == nil:
			return fmt.Errorf("session %q already exists in workspace: %w", session.Name, ErrSessionAlreadyExists)
		case !errors.Is(err, ErrSessionNotFound):
			return fmt.Errorf("failed to check for duplicate session: %w", err)
		}
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	for _, action := range actions {
		action.SessionID = session.ID
		if err := s.sessionActionStore.Add(action); err != nil {
			slog.Error("failed to persist session action", "session", session.ID, "trigger", action.Trigger, "error", err)
		}
	}

	if session.WorkspaceID != 0 {
		if err := s.workspaceService.Touch(ctx, session.WorkspaceID); err != nil {
			slog.Warn("failed to touch workspace last-used timestamp", "workspace", session.WorkspaceID, "error", err)
		}
	}

	go s.runSetup(session.ID, ws, tmuxName, branchName, baseBranchName, createWorktree)

	return nil
}

func (s *SessionService) runSetup(sessionID uint, ws *workspace.Workspace, tmuxName string, branchName string, baseBranchName string, createWorktree bool) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	wsName := ""
	if ws != nil {
		wsName = ws.Name
	}
	ctx = slogctx.Append(ctx,
		"session", sess.ID,
		"ws", wsName,
		"branch", branchName,
		"base_branch", baseBranchName,
	)
	slog.InfoContext(ctx, "session setup: start", "tmux", tmuxName, "create_worktree", createWorktree, "timeout", s.setupTimeout)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "session setup: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
	}()

	markBroken := func(stage string, err error) {
		var msg string
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("%s timed out after %s", stage, s.setupTimeout)
		} else {
			msg = fmt.Sprintf("%s failed: %v", stage, err)
		}
		if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
		}
	}

	var worktreeCreated bool
	var worktreePath string
	var setupWarning string

	hasBranch := ws != nil && ws.IsGitRepo && (branchName != "" || baseBranchName != "")
	if hasBranch {
		if err := s.setupBranch(ctx, ws, branchName, baseBranchName); err != nil {
			var w SetupWarning
			if errors.As(err, &w) {
				setupWarning = w.Message
				sess.StatusError = w.Message
				if updateErr := s.store.Update(sess); updateErr != nil {
					slog.WarnContext(ctx, "failed to persist setup warning", "error", updateErr)
				}
			} else {
				markBroken("branch setup", err)
				return
			}
		}

		finalBranchName := branchName
		if baseBranchName != "" {
			finalBranchName = s.branchPrefix + sess.Name
		}
		if ws.RepoID != nil {
			branch, err := tracedOp(ctx, "find-or-create-branch", func() (*git.Branch, error) {
				return s.gitService.FindOrCreateBranch(ctx, finalBranchName, *ws.RepoID)
			}, "name", finalBranchName)
			if err != nil {
				markBroken("find-or-create-branch", err)
				return
			}
			sess.BranchID = &branch.ID
			if updateErr := s.store.Update(sess); updateErr != nil {
				markBroken("persist branch id", updateErr)
				return
			}
		}

		if createWorktree {
			created, wtPath, err := s.setupWorktree(ctx, sess, ws, branchName, baseBranchName)
			if err != nil {
				markBroken("worktree setup", err)
				return
			}
			worktreeCreated = created
			worktreePath = wtPath

			if err := s.setupWorktreeInit(ctx, sess, ws, worktreeCreated, worktreePath, branchName, baseBranchName); err != nil {
				slog.Warn("worktree init failed, continuing", "error", err)
			}
		}
	}

	sessionRoot := worktreePath
	if sessionRoot == "" && ws != nil {
		sessionRoot = ws.Path
	}
	if sessionRoot != "" {
		sess.SessionRoot = sessionRoot
		if updateErr := s.store.Update(sess); updateErr != nil {
			slog.WarnContext(ctx, "failed to persist session root", "error", updateErr)
		}
	}

	if ws != nil {
		existing, _ := s.sessionWorkspaceStore.ListBySessionID(sess.ID)
		if len(existing) == 0 {
			sw := &SessionWorkspace{
				SessionID:    sess.ID,
				WorkspaceID:  ws.ID,
				BranchID:     sess.BranchID,
				WorktreePath: worktreePath,
				Position:     0,
			}
			if err := s.sessionWorkspaceStore.Add(sw); err != nil {
				slog.WarnContext(ctx, "failed to add session workspace junction", "error", err)
			}
		}
	}

	if err := s.setupTmux(ctx, sess, tmuxName, sessionRoot); err != nil {
		markBroken("tmux setup", err)
		return
	}

	sess.Status = StatusActive
	if setupWarning == "" {
		sess.StatusError = ""
	}
	if err := s.store.Update(sess); err != nil {
		markBroken("persist active status", err)
		return
	}
}

func (s *SessionService) setupBranch(ctx context.Context, ws *workspace.Workspace, branchName string, baseBranchName string) error {
	pullBranch := branchName
	if baseBranchName != "" {
		pullBranch = baseBranchName
	}

	hasRemote, err := tracedOp(ctx, "git ls-remote", func() (bool, error) {
		return s.gitService.HasRemoteBranch(ctx, ws.Path, pullBranch)
	}, "branch", pullBranch)
	if err != nil {
		return fmt.Errorf("failed to check remote branch %q: %v", pullBranch, err)
	}

	if !hasRemote {
		hasLocal, err := tracedOp(ctx, "git rev-parse", func() (bool, error) {
			return s.gitService.HasBranch(ctx, ws.Path, pullBranch)
		}, "branch", pullBranch)
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
		if ws.IsBare {
			if err := traceOp(ctx, "git fetch (bare)", func() error {
				return s.gitService.Fetch(ctx, ws.Path, pullBranch)
			}, "branch", pullBranch); err != nil {
				return SetupWarning{fmt.Sprintf("branch not fetched: %v", err)}
			}
			return nil
		}
		checkPath = ws.Path
	}

	dirty, err := tracedOp(ctx, "git status (dirty check)", func() (bool, error) {
		return s.gitService.IsDirty(ctx, checkPath)
	}, "path", checkPath)
	if err != nil {
		return fmt.Errorf("failed to check dirty state for %q: %v", pullBranch, err)
	}
	if dirty {
		return SetupWarning{fmt.Sprintf("branch not pulled: %q has uncommitted changes", pullBranch)}
	}

	if err := traceOp(ctx, "git pull", func() error {
		return s.gitService.Pull(ctx, checkPath, pullBranch)
	}, "branch", pullBranch, "path", checkPath); err != nil {
		return SetupWarning{fmt.Sprintf("branch not pulled: %v", err)}
	}

	return nil
}

func (s *SessionService) setupWorktree(ctx context.Context, sess *Session, ws *workspace.Workspace, branchName string, baseBranchName string) (bool, string, error) {
	finalBranchName := branchName
	if baseBranchName != "" {
		finalBranchName = s.branchPrefix + sess.Name
	}

	var branchID, repoID uint
	if sess.BranchID != nil {
		branchID = *sess.BranchID
	}
	if ws.RepoID != nil {
		repoID = *ws.RepoID
	}

	var (
		created bool
		path    string
	)
	err := traceOp(ctx, "git worktree add", func() error {
		var e error
		created, path, e = s.gitService.SetupWorktree(ctx, ws.Path, finalBranchName, baseBranchName, branchID, repoID)
		return e
	}, "branch", finalBranchName, "base", baseBranchName)
	return created, path, err
}

func (s *SessionService) setupTmux(ctx context.Context, sess *Session, tmuxName string, sessionRoot string) error {
	if s.tmuxService.HasSession(tmuxName) {
		if sess.TmuxSessionID == nil {
			if ts, err := s.tmuxService.GetSessionByName(tmuxName); err == nil {
				sess.TmuxSessionID = &ts.ID
			}
		}
		slog.InfoContext(ctx, "setup tmux: reusing existing session", "tmux", tmuxName)
		return nil
	}

	if sess.TmuxSessionID != nil {
		if err := traceOp(ctx, "tmux recreate-session", func() error {
			return s.tmuxService.RecreateSession(*sess.TmuxSessionID)
		}, "tmux", tmuxName, "tmux_id", *sess.TmuxSessionID); err != nil {
			return fmt.Errorf("failed to recreate tmux session: %v", err)
		}
		return nil
	}

	startDir := sessionRoot
	if startDir == "" {
		startDir = s.resolveStartDir(ctx, sess)
	}
	env := map[string]string{envSessionID: fmt.Sprintf("%d", sess.ID)}

	ts, err := tracedOp(ctx, "tmux new-session", func() (*utmux.TmuxSession, error) {
		return s.tmuxService.CreateSession(tmuxName, startDir, env)
	}, "tmux", tmuxName, "start_dir", startDir)
	if err != nil {
		if !s.tmuxService.HasSession(tmuxName) {
			return fmt.Errorf("failed to create tmux session: %v", err)
		}
		ts, err = s.tmuxService.GetOrTrackSession(tmuxName, startDir, env)
		if err != nil {
			return fmt.Errorf("failed to create tmux session: %v", err)
		}
	} else {
		if actions, err := s.sessionActionStore.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate); err != nil {
			slog.Warn("failed to load session actions", "session", sess.ID, "error", err)
		} else if len(actions) > 0 {
			slog.InfoContext(ctx, "setup tmux: dispatching on-create actions", "tmux", tmuxName, "count", len(actions))
			go executeSessionActions(actions, s.tmuxService, s.sessionActionStore, tmuxName, startDir)
		}
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
		envWorktreePath + "=" + worktreePath,
		envBranch + "=" + finalBranchName,
		envSessionName + "=" + sess.Name,
	}
	if ws != nil {
		env = append(env,
			envWorkspaceName+"="+ws.Name,
			envWorkspacePath+"="+ws.Path,
		)
	}

	scripts := []string{
		filepath.Join(s.configDir, worktreeSetupName),
	}
	if ws != nil {
		scripts = append(scripts, filepath.Join(ws.Path, ".utena", worktreeSetupName))
	}

	var warnings []string
	for _, script := range scripts {
		err := traceOp(ctx, "worktree-setup script", func() error {
			return s.runScript(ctx, script, worktreePath, env)
		}, "script", script)
		if err != nil {
			slog.Warn("worktree setup script failed", "script", script, "error", err)
			warnings = append(warnings, err.Error())
		}
	}

	if ws != nil && ws.IsBare {
		if err := claudesettings.EnsureWorkspaceRoot(ws.Path); err != nil {
			slog.Warn("ensure workspace claude settings failed", "workspace", ws.Name, "error", err)
			warnings = append(warnings, err.Error())
		} else if err := claudesettings.LinkWorktree(ws.Path, worktreePath); err != nil {
			slog.Warn("link worktree claude settings failed", "worktree", worktreePath, "error", err)
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
	isWarned := sess.Status == StatusActive && sess.StatusError != ""
	if sess.Status != StatusBroken && !isWarned {
		return nil, ErrSessionNotBroken
	}

	sess.Status = StatusCreating
	sess.StatusError = ""
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("failed to mark session for repair: %w", err)
	}

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

	if sess.IsMulti() {
		tmuxName := SanitizeTmuxName(sess.Name)
		go s.runMultiRepair(sess.ID, tmuxName)
		return sess, nil
	}

	tmuxName := s.computeTmuxName(sess, ws)
	hasWorktree := sess.BranchID != nil && s.gitService.HasWorktree(*sess.BranchID)
	go s.runSetup(sess.ID, ws, tmuxName, branchName, baseBranchName, hasWorktree)
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

	s.ensureSessionWorktrees(ctx, session)

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
			if !s.tmuxService.HasSession(tmuxName) {
				return nil, fmt.Errorf("failed to revive tmux session: %w", createErr)
			}
			ts, createErr = s.tmuxService.GetOrTrackSession(tmuxName, startDir, env)
			if createErr != nil {
				return nil, fmt.Errorf("failed to revive tmux session: %w", createErr)
			}
		} else {
			if actions, err := s.sessionActionStore.ListBySessionIDAndTrigger(session.ID, TriggerOnCreate); err != nil {
				slog.Warn("failed to load session actions", "session", session.ID, "error", err)
			} else if len(actions) > 0 {
				go executeSessionActions(actions, s.tmuxService, s.sessionActionStore, tmuxName, startDir)
			}
		}
		session.TmuxSessionID = &ts.ID
		session.Status = StatusActive
		session.StatusError = ""
	}

	if session.IsAttached {
		slog.Info("skipping activation for already attached session", "session", id)
		return session, nil
	}

	if allSessions, err := s.store.List(); err == nil {
		for _, existing := range allSessions {
			if existing.IsAttached {
				slog.Info("clearing attached flag", "session", existing.ID)
				existing.IsAttached = false
				if updateErr := s.store.Update(&existing); updateErr != nil {
					slog.Warn("failed to clear attached flag", "session", existing.ID, "error", updateErr)
				}
			}
		}
	} else {
		slog.Warn("failed to list sessions while clearing attached flags", "error", err)
	}

	session.LastUsedAt = time.Now()
	session.IsAttached = true

	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	for _, wsID := range sessionWorkspaceIDs(session) {
		if err := s.workspaceService.Touch(ctx, wsID); err != nil {
			slog.Warn("failed to touch workspace last-used timestamp", "workspace", wsID, "error", err)
		}
	}

	if err := s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: tmuxName},
	}); err != nil {
		slog.Warn("failed to publish session activated event", "session", session.ID, "error", err)
	}

	return session, nil
}

func (s *SessionService) DeleteSession(ctx context.Context, id uint, deleteBranch bool, force bool) error {
	session, err := s.store.GetByID(id)
	if err != nil {
		return err
	}

	if session.IsAttached {
		return ErrSessionAttached
	}

	if session.Status == StatusCreating && !force {
		return common.NewInvalidRequest("cannot delete session while it is being created")
	}

	s.cleanupSessionBranches(ctx, session, deleteBranch)
	s.cleanupSessionRootDir(session)

	if session.TmuxSessionID != nil {
		if err := s.tmuxService.KillSession(*session.TmuxSessionID); err != nil {
			slog.Warn("failed to kill tmux session", "error", err)
		}
		session.TmuxSessionID = nil
	}

	session.Status = StatusDeleted

	return s.store.Update(session)
}

// cleanupSessionRootDir removes the SessionRoot dir on disk when utena owns it
// (i.e. it lives under the configured sessionsRoot). For single-workspace
// sessions, SessionRoot is either a worktree dir under a repo (cleaned up by
// gitService.CleanupBranch) or the workspace path itself when no worktree was
// created — neither lives under sessionsRoot, so this helper is a no-op.
func (s *SessionService) cleanupSessionRootDir(sess *Session) {
	if sess.SessionRoot == "" || s.sessionsRoot == "" {
		return
	}
	rel, err := filepath.Rel(s.sessionsRoot, sess.SessionRoot)
	if err != nil || rel == "" || rel == "." || strings.HasPrefix(rel, "..") {
		return
	}
	if err := os.RemoveAll(sess.SessionRoot); err != nil {
		slog.Warn("failed to remove session root", "path", sess.SessionRoot, "error", err)
	}
}

func (s *SessionService) cleanupSessionBranches(ctx context.Context, session *Session, deleteBranch bool) {
	if len(session.Workspaces) > 0 {
		for i := range session.Workspaces {
			sw := &session.Workspaces[i]
			if sw.GitBranch == nil || sw.Workspace == nil {
				continue
			}
			if err := s.gitService.CleanupBranch(ctx, sw.GitBranch, sw.Workspace.Path, deleteBranch); err != nil {
				slog.Warn("failed to cleanup branch", "workspace_id", sw.WorkspaceID, "error", err)
			}
		}
		return
	}
	if session.BranchID != nil && session.GitBranch != nil && session.WorkspaceID != 0 {
		ws, err := s.workspaceService.GetWorkspace(ctx, session.WorkspaceID)
		if err == nil {
			if err := s.gitService.CleanupBranch(ctx, session.GitBranch, ws.Path, deleteBranch); err != nil {
				slog.Warn("failed to cleanup branch", "error", err)
			}
			return
		}
		slog.Warn("workspace not found during cleanup, skipping worktree/branch removal", "workspace_id", session.WorkspaceID)
	}
}

func (s *SessionService) computeTmuxName(sess *Session, ws *workspace.Workspace) string {
	if ws != nil {
		return BuildTmuxSessionName(ws.Name, sess.Name)
	}
	return SanitizeTmuxName(sess.Name)
}

func (s *SessionService) nameFromBranch(branchName string) string {
	if s.branchPrefix == "" {
		return branchName
	}
	stripped := strings.TrimPrefix(branchName, s.branchPrefix)
	if stripped == "" {
		return branchName
	}
	return stripped
}

func (s *SessionService) resolveStartDir(ctx context.Context, session *Session) string {
	if session.SessionRoot != "" {
		return session.SessionRoot
	}
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

	if _, err := s.RefreshSession(ctx, sess.ID); err != nil {
		slog.Warn("failed to refresh session after tmux create", "session", sess.ID, "error", err)
	}
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

	sessions, err := s.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions while clearing attached flags: %w", err)
	}
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
	sessions, err := s.store.List()
	if err != nil {
		slog.Error("failed to list sessions for reconciliation", "error", err)
		return
	}
	for _, sess := range sessions {
		if sess.Status != StatusDeleted {
			if _, err := s.RefreshSession(ctx, sess.ID); err != nil {
				slog.Warn("failed to refresh session during reconcile", "session", sess.ID, "error", err)
			}
		}
	}
}

func (s *SessionService) ArchiveSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status != StatusActive && sess.Status != StatusInactive && sess.Status != StatusCompleted {
		return nil, fmt.Errorf("cannot archive session in status %s", sess.Status)
	}

	s.cleanupSessionBranches(ctx, sess, false)
	s.cleanupSessionRootDir(sess)

	if sess.TmuxSessionID != nil {
		if err := s.tmuxService.KillSession(*sess.TmuxSessionID); err != nil {
			slog.Warn("failed to kill tmux session during archive", "session", sess.ID, "error", err)
		}
		sess.TmuxSessionID = nil
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
			if err := s.dismissedPRStore.Add(&DismissedPR{
				PullRequestID: pr.ID,
				DismissedAt:   time.Now(),
			}); err != nil {
				slog.Warn("failed to record dismissed PR", "pr", pr.ID, "error", err)
			}
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

	gitHealthy := s.isSessionGitHealthy(ctx, sess)

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

	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("failed to persist reconciled session: %w", err)
	}
	return sess, nil
}

func sessionWorkspaceIDs(sess *Session) []uint {
	if len(sess.Workspaces) > 0 {
		out := make([]uint, 0, len(sess.Workspaces))
		for i := range sess.Workspaces {
			if sess.Workspaces[i].WorkspaceID != 0 {
				out = append(out, sess.Workspaces[i].WorkspaceID)
			}
		}
		return out
	}
	if sess.WorkspaceID != 0 {
		return []uint{sess.WorkspaceID}
	}
	return nil
}

func (s *SessionService) ensureSessionWorktrees(ctx context.Context, sess *Session) {
	if len(sess.Workspaces) > 0 {
		for i := range sess.Workspaces {
			sw := &sess.Workspaces[i]
			if sw.GitBranch == nil || sw.Workspace == nil || sw.WorktreePath == "" {
				continue
			}
			if _, err := os.Stat(sw.WorktreePath); err == nil {
				continue
			}
			repoID := uint(0)
			if sw.Workspace.RepoID != nil {
				repoID = *sw.Workspace.RepoID
			}
			if err := s.gitService.PruneWorktrees(ctx, sw.Workspace.Path); err != nil {
				slog.WarnContext(ctx, "prune worktrees before ensure failed", "workspace", sw.Workspace.Name, "error", err)
			}
			if _, _, err := s.gitService.SetupWorktreeAt(ctx, sw.Workspace.Path, sw.GitBranch.Name, "", sw.GitBranch.ID, repoID, sw.WorktreePath); err != nil {
				slog.WarnContext(ctx, "failed to ensure session worktree on activation", "session", sess.ID, "workspace", sw.Workspace.Name, "branch", sw.GitBranch.Name, "error", err)
			}
		}
		if sess.IsMulti() && sess.SessionRoot != "" {
			if err := os.MkdirAll(sess.SessionRoot, 0o755); err != nil {
				slog.WarnContext(ctx, "failed to ensure session root", "session", sess.ID, "path", sess.SessionRoot, "error", err)
			}
		}
		return
	}
	if sess.BranchID != nil && sess.GitBranch != nil && sess.WorkspaceID != 0 {
		if ws, wsErr := s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID); wsErr == nil && ws != nil {
			if _, wtErr := s.gitService.EnsureWorktree(ctx, sess.GitBranch, ws.Path); wtErr != nil {
				slog.WarnContext(ctx, "failed to ensure worktree on activation", "session", sess.ID, "branch", sess.GitBranch.Name, "error", wtErr)
			}
		}
	}
}

func (s *SessionService) isSessionGitHealthy(ctx context.Context, sess *Session) bool {
	if len(sess.Workspaces) > 0 {
		for i := range sess.Workspaces {
			sw := &sess.Workspaces[i]
			if sw.GitBranch == nil || sw.Workspace == nil {
				continue
			}
			if !s.gitService.IsHealthy(ctx, sw.GitBranch, sw.Workspace.Path) {
				return false
			}
		}
		return true
	}
	if sess.BranchID != nil && sess.GitBranch != nil {
		ws, _ := s.workspaceService.GetWorkspace(ctx, sess.WorkspaceID)
		if ws == nil {
			return true
		}
		return s.gitService.IsHealthy(ctx, sess.GitBranch, ws.Path)
	}
	return true
}

func (s *SessionService) markSessionBroken(sess *Session, statusError string) error {
	sess.Status = StatusBroken
	sess.StatusError = statusError
	return s.store.Update(sess)
}

func (s *SessionService) handlePRUpdated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(git.PRUpdatedEvent)
	if !ok {
		return nil
	}

	pr := data.PullRequest
	if pr.HeadBranchID == nil {
		return nil
	}

	isNew := data.Previous == nil
	newlyAssigned := pr.IsAssignedToMe && pr.State == git.PRStateOpen && (isNew || !data.Previous.IsAssignedToMe)
	newlyMerged := pr.State == git.PRStateMerged && (isNew || data.Previous.State != git.PRStateMerged)

	if newlyAssigned {
		s.maybeCreatePendingSession(ctx, data)
	}

	if newlyMerged {
		s.maybeCompleteSession(ctx, pr)
	}

	return nil
}

func (s *SessionService) maybeCreatePendingSession(ctx context.Context, data git.PRUpdatedEvent) {
	pr := data.PullRequest

	_, err := s.store.GetByBranchID(*pr.HeadBranchID)
	if err == nil {
		return
	}

	if s.dismissedPRStore != nil && s.dismissedPRStore.IsDismissed(pr.ID) {
		return
	}

	ws, err := s.workspaceService.GetWorkspaceByRepoID(ctx, data.Repo.ID)
	if err != nil {
		slog.Debug("no workspace for repo, skipping PR session", "repo_id", data.Repo.ID)
		return
	}

	branch, err := s.gitService.GetBranch(*pr.HeadBranchID)
	if err != nil {
		slog.Warn("failed to get branch for PR session", "error", err)
		return
	}

	sess := &Session{
		WorkspaceID: ws.ID,
		BranchID:    pr.HeadBranchID,
	}

	prompt := fmt.Sprintf(
		"/review the latest changes for the PR %s are checked out in the current working directory. "+
			"Review the changes and prepare an initial feedback report (in memory, don't write to a file). "+
			"Then for each piece of feedback spawn a subagent to play devil's advocate and verify the validity of the feedback. "+
			"Once that is done prepare a final feedback report for me incorporating the subagent feedback (in memory again). "+
			"The final report should just reflect the final state of feedback and should not reference any initial feedback that was dismissed by the subagents or make any reference at all to the process.",
		pr.HTMLURL,
	)
	reviewAction := &SessionAction{
		Trigger: TriggerOnCreate,
		Type:    SessionActionTypeClaude,
		Options: marshalOptions(ClaudeActionOptions{Prompt: prompt}),
	}

	if err := s.CreateSession(ctx, sess, branch.Name, "", false, reviewAction); err != nil {
		slog.Warn("failed to create session for PR", "pr", pr.Number, "error", err)
		return
	}

	slog.Info("activating session for assigned PR", "pr", pr.Number, "session", sess.ID, "branch", branch.Name)
}

func (s *SessionService) maybeCompleteSession(_ context.Context, pr *git.PullRequest) {
	sess, err := s.store.GetByBranchID(*pr.HeadBranchID)
	if err != nil {
		return
	}

	if sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusCompleted {
		return
	}

	sess.Status = StatusCompleted
	if err := s.store.Update(sess); err != nil {
		slog.Warn("failed to mark session completed", "session", sess.ID, "error", err)
	} else {
		slog.Info("marked session completed after PR merge", "session", sess.ID, "pr", pr.Number)
	}
}
