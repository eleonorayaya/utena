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
	sessionWorktreeStore  *SessionWorktreeStore
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

func NewSessionService(store *SessionStore, sessionWorkspaceStore *SessionWorkspaceStore, sessionWorktreeStore *SessionWorktreeStore, dismissedPRStore *DismissedPRStore, sessionActionStore *SessionActionStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, tmuxService *utmux.TmuxService, bus eventbus.EventBus, branchPrefix string, configDir string, sessionsRoot string) *SessionService {
	return &SessionService{
		store:                 store,
		sessionWorkspaceStore: sessionWorkspaceStore,
		sessionWorktreeStore:  sessionWorktreeStore,
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
	sessions, err := s.store.List()
	if err != nil {
		slog.Warn("startup: failed to list sessions", "error", err)
		return nil
	}
	s.markOrphanedSessionsBroken(sessions)
	s.backfillLegacyTmuxRecords(sessions)
	s.backfillSessionWorktrees(ctx, sessions)
	s.recoverStuckCreatingSessions(sessions)
	s.reconcileTmuxState(ctx, sessions)
	return nil
}

// backfillSessionWorktrees walks every existing SessionWorkspace junction row
// and materialises a corresponding Worktree record + SessionWorktree junction
// row when one does not already exist. Pre-existing data is the source of
// truth: the old WorktreePath column drives Worktree.Path, and BranchID drives
// Worktree.BranchID.
//
// Idempotent: a SessionWorktree row keyed on (SessionID, WorktreeID) is only
// inserted once thanks to the uniqueIndex.
func (s *SessionService) backfillSessionWorktrees(ctx context.Context, sessions []Session) {
	for i := range sessions {
		sess := &sessions[i]
		if len(sess.Worktrees) > 0 {
			continue
		}
		for j := range sess.Workspaces {
			sw := &sess.Workspaces[j]
			if sw.BranchID == nil || sw.WorktreePath == "" || sw.Workspace == nil {
				continue
			}
			repoID := uint(0)
			if sw.Workspace.RepoID != nil {
				repoID = *sw.Workspace.RepoID
			}
			wsID := sw.WorkspaceID
			wt, err := s.gitService.RegisterPendingWorktree(*sw.BranchID, repoID, &wsID, sw.WorktreePath)
			if err != nil {
				slog.WarnContext(ctx, "backfill: register pending worktree failed", "session", sess.ID, "workspace", sw.WorkspaceID, "error", err)
				continue
			}
			if _, err := os.Stat(sw.WorktreePath); err == nil {
				if wt.Status != git.WorktreeStatusPresent {
					wt.Status = git.WorktreeStatusPresent
					if updateErr := s.gitService.UpdateWorktree(wt); updateErr != nil {
						slog.WarnContext(ctx, "backfill: failed to mark worktree present", "session", sess.ID, "error", updateErr)
					}
				}
			} else if wt.Status == git.WorktreeStatusPending {
				if err := s.gitService.MarkWorktreeMissing(wt); err != nil {
					slog.WarnContext(ctx, "backfill: failed to mark worktree missing", "session", sess.ID, "error", err)
				}
			}
			swt := &SessionWorktree{SessionID: sess.ID, WorktreeID: wt.ID, Position: sw.Position}
			if err := s.sessionWorktreeStore.Add(swt); err != nil {
				slog.DebugContext(ctx, "backfill: session-worktree row already exists", "session", sess.ID, "worktree", wt.ID, "error", err)
			}
		}
	}
}

func (s *SessionService) backfillLegacyTmuxRecords(sessions []Session) {
	for i := range sessions {
		sess := &sessions[i]
		if sess.TmuxSessionID != nil {
			continue
		}
		if sess.Status == StatusDeleted || sess.Status == StatusArchived {
			continue
		}
		if len(sess.Workspaces) == 0 {
			continue
		}
		var tmuxName, startDir string
		if sess.IsMulti() {
			tmuxName = SanitizeTmuxName(sess.Name)
			startDir = sess.SessionRoot
		} else {
			ws := sess.Workspaces[0].Workspace
			if ws == nil {
				continue
			}
			tmuxName = BuildTmuxSessionName(ws.Name, sess.Name)
			startDir = sess.SessionRoot
			if startDir == "" {
				startDir = sess.Workspaces[0].WorktreePath
			}
		}
		env := map[string]string{envSessionID: fmt.Sprintf("%d", sess.ID)}
		record, err := s.tmuxService.RegisterPending(tmuxName, startDir, env)
		if err != nil {
			slog.Warn("backfill: failed to register pending tmux record", "session", sess.ID, "error", err)
			continue
		}
		sess.TmuxSessionID = &record.ID
		if err := s.store.Update(sess); err != nil {
			slog.Warn("backfill: failed to persist tmux session id", "session", sess.ID, "error", err)
		}
	}
}

func (s *SessionService) markOrphanedSessionsBroken(sessions []Session) {
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusBroken {
			continue
		}
		if len(sess.Workspaces) > 0 || len(sess.Worktrees) > 0 {
			continue
		}
		if err := s.markSessionBroken(sess, "session predates multi-workspace migration — please recreate"); err != nil {
			slog.Warn("orphan check: failed to mark session broken", "session", sess.ID, "error", err)
		}
	}
}

func (s *SessionService) recoverStuckCreatingSessions(sessions []Session) {
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

func (s *SessionService) CreateSession(ctx context.Context, session *Session, workspaceID uint, branchName string, baseBranchName string, actions ...*SessionAction) error {
	if workspaceID == 0 {
		return common.NewInvalidRequest("workspace_id is required")
	}
	ws, err := s.workspaceService.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if !ws.IsGitRepo {
		return common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name))
	}

	if session.TodoID != nil && baseBranchName == "" && branchName == "" {
		baseBranchName = "main"
	}

	if branchName == "" && baseBranchName == "" {
		return common.NewInvalidRequest("branch or base_branch is required")
	}

	if session.Name == "" && branchName != "" {
		session.Name = s.nameFromBranch(branchName)
	}
	if session.Name == "" {
		return common.NewInvalidRequest("session name or branch is required")
	}
	tmuxName := BuildTmuxSessionName(ws.Name, session.Name)
	finalBranchName := branchName
	if baseBranchName != "" {
		finalBranchName = s.branchPrefix + session.Name
	}
	worktreePath := s.gitService.WorktreePath(ws.Path, finalBranchName)

	session.WorkspaceID = ws.ID
	session.Status = StatusCreating
	session.SessionRoot = worktreePath

	if session.LastUsedAt.IsZero() {
		session.LastUsedAt = time.Now()
	}

	_, err = s.store.GetByWorkspaceAndName(ws.ID, session.Name, StatusDeleted, StatusArchived)
	switch {
	case err == nil:
		return fmt.Errorf("session %q already exists in workspace: %w", session.Name, ErrSessionAlreadyExists)
	case !errors.Is(err, ErrSessionNotFound):
		return fmt.Errorf("failed to check for duplicate session: %w", err)
	}

	if err := s.store.Add(session); err != nil {
		return err
	}

	sw := &SessionWorkspace{
		SessionID:    session.ID,
		WorkspaceID:  ws.ID,
		WorktreePath: worktreePath,
		Position:     0,
	}
	if err := s.sessionWorkspaceStore.Add(sw); err != nil {
		if delErr := s.store.Delete(session.ID); delErr != nil {
			slog.WarnContext(ctx, "failed to roll back session after junction add failure", "session", session.ID, "error", delErr)
		}
		return fmt.Errorf("add session-workspace junction: %w", err)
	}

	if err := s.eagerCreateWorktree(ctx, session.ID, ws, finalBranchName, worktreePath, 0); err != nil {
		slog.WarnContext(ctx, "failed to eagerly create worktree record", "session", session.ID, "workspace", ws.Name, "error", err)
	} else {
		sw.BranchID = nil
		if branchID, err := s.lookupBranchID(ctx, ws, finalBranchName); err == nil {
			sw.BranchID = branchID
			if updateErr := s.sessionWorkspaceStore.Update(sw); updateErr != nil {
				slog.WarnContext(ctx, "failed to update legacy junction with branch id", "session", session.ID, "error", updateErr)
			}
		}
	}

	for _, action := range actions {
		action.SessionID = session.ID
		if err := s.sessionActionStore.Add(action); err != nil {
			slog.Error("failed to persist session action", "session", session.ID, "trigger", action.Trigger, "error", err)
		}
	}

	env := map[string]string{envSessionID: fmt.Sprintf("%d", session.ID)}
	tmuxRecord, err := s.tmuxService.RegisterPending(tmuxName, worktreePath, env)
	if err != nil {
		slog.WarnContext(ctx, "failed to register pending tmux record", "session", session.ID, "tmux", tmuxName, "error", err)
	} else {
		session.TmuxSessionID = &tmuxRecord.ID
		if err := s.store.Update(session); err != nil {
			slog.WarnContext(ctx, "failed to persist tmux session id", "session", session.ID, "error", err)
		}
	}

	if err := s.workspaceService.Touch(ctx, ws.ID); err != nil {
		slog.Warn("failed to touch workspace last-used timestamp", "workspace", ws.ID, "error", err)
	}

	go s.runSetup(session.ID, tmuxName, branchName, baseBranchName)

	return nil
}

// eagerCreateWorktree resolves the repo for the workspace, finds-or-creates the
// branch record, and creates a Worktree record in pending status plus the
// SessionWorktree junction row. It is idempotent: if the worktree record
// already exists for the branch, it is reused (Status preserved).
func (s *SessionService) eagerCreateWorktree(ctx context.Context, sessionID uint, ws *workspace.Workspace, branchName string, worktreePath string, position int) error {
	if branchName == "" {
		return fmt.Errorf("branch name is required")
	}
	repoID, err := s.resolveRepoID(ctx, ws)
	if err != nil {
		return fmt.Errorf("resolve repo: %w", err)
	}
	branch, err := s.gitService.FindOrCreateBranch(ctx, branchName, repoID)
	if err != nil {
		return fmt.Errorf("find-or-create branch: %w", err)
	}
	wsID := ws.ID
	wt, err := s.gitService.RegisterPendingWorktree(branch.ID, repoID, &wsID, worktreePath)
	if err != nil {
		return fmt.Errorf("register pending worktree: %w", err)
	}
	swt := &SessionWorktree{
		SessionID:  sessionID,
		WorktreeID: wt.ID,
		Position:   position,
	}
	if err := s.sessionWorktreeStore.Add(swt); err != nil {
		return fmt.Errorf("add session-worktree junction: %w", err)
	}
	return nil
}

// lookupBranchID returns the branch ID for the given workspace+branch name pair
// without creating one if absent.
func (s *SessionService) lookupBranchID(ctx context.Context, ws *workspace.Workspace, branchName string) (*uint, error) {
	repoID, err := s.resolveRepoID(ctx, ws)
	if err != nil {
		return nil, err
	}
	branch, err := s.gitService.FindOrCreateBranch(ctx, branchName, repoID)
	if err != nil {
		return nil, err
	}
	return &branch.ID, nil
}

// syncLegacyJunction backfills the SessionWorkspace junction for a given
// worktree+branch — kept while readers transition off the legacy junction.
func (s *SessionService) syncLegacyJunction(ctx context.Context, sess *Session, wt *git.Worktree, branch *git.Branch) {
	if wt == nil || branch == nil {
		return
	}
	for i := range sess.Workspaces {
		sw := &sess.Workspaces[i]
		if wt.WorkspaceID != nil && sw.WorkspaceID != *wt.WorkspaceID {
			continue
		}
		if wt.WorkspaceID == nil && sw.WorktreePath != "" && sw.WorktreePath != wt.Path {
			continue
		}
		changed := false
		if sw.BranchID == nil || *sw.BranchID != branch.ID {
			id := branch.ID
			sw.BranchID = &id
			changed = true
		}
		if sw.WorktreePath == "" || sw.WorktreePath != wt.Path {
			sw.WorktreePath = wt.Path
			changed = true
		}
		if changed {
			if err := s.sessionWorkspaceStore.Update(sw); err != nil {
				slog.WarnContext(ctx, "failed to sync legacy session-workspace junction", "session", sess.ID, "error", err)
			}
		}
		return
	}
}

// resolveWorktreeWorkspace returns the workspace bound to the given worktree
// record. It first consults the cache, then the worktree's WorkspaceID, then
// falls back to a repo-id lookup. The result is cached for the lifetime of the
// caller-supplied map.
func (s *SessionService) resolveWorktreeWorkspace(ctx context.Context, wt *git.Worktree, cache map[uint]*workspace.Workspace) (*workspace.Workspace, error) {
	if wt == nil {
		return nil, fmt.Errorf("worktree is nil")
	}
	if wt.WorkspaceID != nil {
		if ws, ok := cache[*wt.WorkspaceID]; ok {
			return ws, nil
		}
		ws, err := s.workspaceService.GetWorkspace(ctx, *wt.WorkspaceID)
		if err == nil {
			cache[*wt.WorkspaceID] = ws
			return ws, nil
		}
	}
	if wt.RepoID != 0 {
		ws, err := s.workspaceService.GetWorkspaceByRepoID(ctx, wt.RepoID)
		if err == nil {
			if wt.WorkspaceID == nil {
				wsID := ws.ID
				wt.WorkspaceID = &wsID
				if updateErr := s.gitService.UpdateWorktree(wt); updateErr != nil {
					slog.WarnContext(ctx, "failed to backfill worktree workspace_id", "worktree", wt.ID, "error", updateErr)
				}
			}
			cache[ws.ID] = ws
			return ws, nil
		}
	}
	return nil, fmt.Errorf("no workspace bound to worktree %d", wt.ID)
}

func (s *SessionService) runSetup(sessionID uint, tmuxName string, branchName string, baseBranchName string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	multi := sess.IsMulti()

	ctx = slogctx.Append(ctx,
		"session", sess.ID,
		"branch", branchName,
		"base_branch", baseBranchName,
		"session_root", sess.SessionRoot,
		"workspace_count", len(sess.Worktrees),
	)
	slog.InfoContext(ctx, "session setup: start", "tmux", tmuxName, "timeout", s.setupTimeout, "multi", multi)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "session setup: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
	}()

	type slotResult struct {
		swt          *SessionWorktree
		wt           *git.Worktree
		ws           *workspace.Workspace
		branch       *git.Branch
		worktreePath string
		created      bool
	}
	var done []slotResult
	var setupWarnings []string
	wsCache := make(map[uint]*workspace.Workspace)

	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			r := done[i]
			if r.worktreePath == "" {
				continue
			}
			if err := s.gitService.RemoveWorktree(ctx, r.ws.Path, r.worktreePath); err != nil {
				slog.WarnContext(ctx, "rollback: failed to remove worktree", "workspace", r.ws.Name, "path", r.worktreePath, "error", err)
			}
		}
		if multi && sess.SessionRoot != "" {
			if err := os.RemoveAll(sess.SessionRoot); err != nil {
				slog.WarnContext(ctx, "rollback: failed to remove session root", "path", sess.SessionRoot, "error", err)
			}
		}
	}

	markBroken := func(stage string, err error) {
		rollback()
		msg := fmt.Sprintf("%s failed: %v", stage, err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("%s timed out after %s", stage, s.setupTimeout)
		}
		if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
		}
	}

	finalBranchName := branchName
	if baseBranchName != "" {
		finalBranchName = s.branchPrefix + sess.Name
	}

	if multi && sess.SessionRoot != "" {
		if err := os.MkdirAll(sess.SessionRoot, 0o755); err != nil {
			markBroken("ensure session root", err)
			return
		}
	}

	if len(sess.Worktrees) == 0 {
		markBroken("missing worktrees", fmt.Errorf("session %d has no SessionWorktree rows", sess.ID))
		return
	}

	for i := range sess.Worktrees {
		swt := &sess.Worktrees[i]
		wt := swt.Worktree
		if wt == nil {
			markBroken("missing worktree", fmt.Errorf("session worktree %d has nil worktree ref", swt.ID))
			return
		}
		ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
		if err != nil {
			markBroken("workspace lookup", err)
			return
		}

		if err := s.setupBranch(ctx, ws, branchName, baseBranchName); err != nil {
			var w SetupWarning
			if errors.As(err, &w) {
				setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", ws.Name, w.Message))
			} else {
				markBroken("branch setup", err)
				return
			}
		}

		repoID, err := s.resolveRepoID(ctx, ws)
		if err != nil {
			markBroken("repo lookup", err)
			return
		}

		branch, err := tracedOp(ctx, "find-or-create-branch", func() (*git.Branch, error) {
			return s.gitService.FindOrCreateBranch(ctx, finalBranchName, repoID)
		}, "name", finalBranchName, "ws", ws.Name)
		if err != nil {
			markBroken("find-or-create-branch", err)
			return
		}

		var (
			created bool
			wtPath  string
		)
		if err := traceOp(ctx, "git worktree add", func() error {
			var e error
			created, wtPath, e = s.gitService.SetupWorktreeAt(ctx, ws.Path, finalBranchName, baseBranchName, branch.ID, repoID, wt.Path)
			return e
		}, "branch", finalBranchName, "ws", ws.Name); err != nil {
			markBroken("worktree setup", err)
			return
		}

		// Re-fetch the worktree record so we see the present-status update
		// applied by SetupWorktreeAt's ensureWorktreeRecord.
		if refreshed, err := s.gitService.GetWorktree(wt.ID); err == nil {
			*wt = *refreshed
		}
		if wt.Path == "" {
			wt.Path = wtPath
		}

		// Keep the legacy SessionWorkspace junction in sync for any
		// reader that has not yet swapped over.
		s.syncLegacyJunction(ctx, sess, wt, branch)

		done = append(done, slotResult{swt: swt, wt: wt, ws: ws, branch: branch, worktreePath: wt.Path, created: created})
	}

	for _, r := range done {
		if !r.created {
			continue
		}
		env := []string{
			envSessionRoot + "=" + sess.SessionRoot,
			envSessionName + "=" + sess.Name,
			envBranch + "=" + finalBranchName,
			envWorktreePath + "=" + r.worktreePath,
			envWorkspaceName + "=" + r.ws.Name,
			envWorkspacePath + "=" + r.ws.Path,
		}
		wsScript := filepath.Join(r.ws.Path, ".utena", worktreeSetupName)
		if err := traceOp(ctx, "worktree-setup script", func() error {
			return s.runScript(ctx, wsScript, r.worktreePath, env)
		}, "script", wsScript, "ws", r.ws.Name); err != nil {
			slog.WarnContext(ctx, "workspace worktree-setup script failed", "ws", r.ws.Name, "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", r.ws.Name, err.Error()))
		}
	}

	anyCreated := false
	for _, r := range done {
		if r.created {
			anyCreated = true
			break
		}
	}
	if anyCreated {
		globalEnv := []string{
			envSessionRoot + "=" + sess.SessionRoot,
			envSessionName + "=" + sess.Name,
			envBranch + "=" + finalBranchName,
		}
		globalScript := filepath.Join(s.configDir, worktreeSetupName)
		if err := traceOp(ctx, "global worktree-setup script", func() error {
			return s.runScript(ctx, globalScript, sess.SessionRoot, globalEnv)
		}, "script", globalScript); err != nil {
			slog.WarnContext(ctx, "global worktree-setup script failed", "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("global: %s", err.Error()))
		}
	}

	if multi {
		gitDirs := make([]string, 0, len(done))
		for _, r := range done {
			gitDirs = append(gitDirs, filepath.Join(r.worktreePath, ".git"))
		}
		if err := claudesettings.EnsureSessionRoot(sess.SessionRoot, gitDirs); err != nil {
			slog.WarnContext(ctx, "ensure session-root claude settings failed", "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("claude-settings: %s", err.Error()))
		}
	} else if len(done) == 1 && done[0].created {
		ws := done[0].ws
		if ws.IsBare {
			if err := claudesettings.EnsureWorkspaceRoot(ws.Path); err != nil {
				slog.Warn("ensure workspace claude settings failed", "workspace", ws.Name, "error", err)
				setupWarnings = append(setupWarnings, err.Error())
			} else if err := claudesettings.LinkWorktree(ws.Path, done[0].worktreePath); err != nil {
				slog.Warn("link worktree claude settings failed", "worktree", done[0].worktreePath, "error", err)
				setupWarnings = append(setupWarnings, err.Error())
			}
		}
	}

	if err := s.setupTmux(ctx, sess, tmuxName, sess.SessionRoot); err != nil {
		markBroken("tmux setup", err)
		return
	}

	sess.Status = StatusActive
	if len(setupWarnings) == 0 {
		sess.StatusError = ""
	} else {
		sess.StatusError = strings.Join(setupWarnings, "; ")
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

func (s *SessionService) setupTmux(ctx context.Context, sess *Session, tmuxName string, sessionRoot string) error {
	if sess.TmuxSessionID != nil {
		record, err := s.tmuxService.GetSession(*sess.TmuxSessionID)
		if err != nil {
			return fmt.Errorf("failed to load tmux record: %v", err)
		}
		isFirstSpawn := record.Status == utmux.TmuxStatusPending

		if s.tmuxService.HasSession(tmuxName) {
			slog.InfoContext(ctx, "setup tmux: reusing existing tmux process", "tmux", tmuxName)
			if _, err := tracedOp(ctx, "tmux spawn-for-record", func() (*utmux.TmuxSession, error) {
				return s.tmuxService.SpawnForRecord(record.ID)
			}, "tmux", tmuxName, "tmux_id", record.ID); err != nil {
				return fmt.Errorf("failed to mark tmux active: %v", err)
			}
		} else {
			if _, err := tracedOp(ctx, "tmux spawn-for-record", func() (*utmux.TmuxSession, error) {
				return s.tmuxService.SpawnForRecord(record.ID)
			}, "tmux", tmuxName, "tmux_id", record.ID); err != nil {
				return fmt.Errorf("failed to spawn tmux session: %v", err)
			}
		}

		if isFirstSpawn {
			s.dispatchOnCreateActions(ctx, sess, tmuxName, record.StartDir)
		}
		return nil
	}

	if s.tmuxService.HasSession(tmuxName) {
		if ts, err := s.tmuxService.GetSessionByName(tmuxName); err == nil {
			sess.TmuxSessionID = &ts.ID
		}
		slog.InfoContext(ctx, "setup tmux: reusing existing session", "tmux", tmuxName)
		return nil
	}

	startDir := sessionRoot
	if startDir == "" {
		startDir = sess.SessionRoot
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
		s.dispatchOnCreateActions(ctx, sess, tmuxName, startDir)
	}
	sess.TmuxSessionID = &ts.ID
	return nil
}

func (s *SessionService) dispatchOnCreateActions(ctx context.Context, sess *Session, tmuxName, startDir string) {
	actions, err := s.sessionActionStore.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	if err != nil {
		slog.WarnContext(ctx, "failed to load session actions", "session", sess.ID, "error", err)
		return
	}
	if len(actions) == 0 {
		return
	}
	slog.InfoContext(ctx, "setup tmux: dispatching on-create actions", "tmux", tmuxName, "count", len(actions))
	go executeSessionActions(actions, s.tmuxService, s.sessionActionStore, tmuxName, startDir)
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

func (s *SessionService) RepairSession(_ context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	isWarned := sess.Status == StatusActive && sess.StatusError != ""
	if sess.Status != StatusBroken && !isWarned {
		return nil, ErrSessionNotBroken
	}

	if len(sess.Worktrees) == 0 && len(sess.Workspaces) == 0 {
		return nil, common.NewInvalidRequest("session has no worktrees; cannot repair — please recreate")
	}

	sess.Status = StatusCreating
	sess.StatusError = ""
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("failed to mark session for repair: %w", err)
	}

	if sess.TmuxSession == nil || sess.TmuxSession.Name == "" {
		return nil, common.NewInvalidRequest("session has no tmux record name; please recreate")
	}
	tmuxName := sess.TmuxSession.Name

	branchName := ""
	if len(sess.Worktrees) > 0 && sess.Worktrees[0].Worktree != nil && sess.Worktrees[0].Worktree.Branch != nil {
		branchName = sess.Worktrees[0].Worktree.Branch.Name
	} else if len(sess.Workspaces) > 0 && sess.Workspaces[0].GitBranch != nil {
		branchName = sess.Workspaces[0].GitBranch.Name
	}
	go s.runSetup(sess.ID, tmuxName, branchName, "")
	return sess, nil
}

func (s *SessionService) UpdateSession(_ context.Context, session *Session) error {
	if _, err := s.store.GetByID(session.ID); err != nil {
		return err
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

	if session.TmuxSession == nil {
		return nil, fmt.Errorf("session has no tmux record; please repair")
	}
	tmuxName := session.TmuxSession.Name

	if !s.tmuxService.HasSession(tmuxName) {
		startDir := session.SessionRoot
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
	if len(session.Worktrees) > 0 {
		wsCache := make(map[uint]*workspace.Workspace)
		for i := range session.Worktrees {
			wt := session.Worktrees[i].Worktree
			if wt == nil || wt.Branch == nil {
				continue
			}
			ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
			if err != nil {
				slog.Warn("cleanup: failed to resolve workspace", "worktree", wt.ID, "error", err)
				continue
			}
			if err := s.gitService.CleanupBranch(ctx, wt.Branch, ws.Path, deleteBranch); err != nil {
				slog.Warn("failed to cleanup branch", "workspace_id", ws.ID, "error", err)
			}
		}
		return
	}
	for i := range session.Workspaces {
		sw := &session.Workspaces[i]
		if sw.GitBranch == nil || sw.Workspace == nil {
			continue
		}
		if err := s.gitService.CleanupBranch(ctx, sw.GitBranch, sw.Workspace.Path, deleteBranch); err != nil {
			slog.Warn("failed to cleanup branch", "workspace_id", sw.WorkspaceID, "error", err)
		}
	}
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

func (s *SessionService) reconcileTmuxState(ctx context.Context, sessions []Session) {
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

	if s.dismissedPRStore != nil {
		branchIDs := s.sessionBranchIDs(sess)
		for _, branchID := range branchIDs {
			for _, pr := range s.gitService.GetPRsForBranch(branchID) {
				if err := s.dismissedPRStore.Add(&DismissedPR{
					PullRequestID: pr.ID,
					DismissedAt:   time.Now(),
				}); err != nil {
					slog.Warn("failed to record dismissed PR", "pr", pr.ID, "error", err)
				}
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
			tmuxAlive = ts.Status == utmux.TmuxStatusActive
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

// sessionBranchIDs returns the branch IDs associated with the session,
// preferring the new SessionWorktree junction and falling back to the legacy
// SessionWorkspace junction for sessions that have not yet been backfilled.
func (s *SessionService) sessionBranchIDs(sess *Session) []uint {
	out := make([]uint, 0)
	if len(sess.Worktrees) > 0 {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.BranchID == 0 {
				continue
			}
			out = append(out, wt.BranchID)
		}
		return out
	}
	for i := range sess.Workspaces {
		sw := &sess.Workspaces[i]
		if sw.BranchID == nil {
			continue
		}
		out = append(out, *sw.BranchID)
	}
	return out
}

func sessionWorkspaceIDs(sess *Session) []uint {
	if len(sess.Worktrees) > 0 {
		out := make([]uint, 0, len(sess.Worktrees))
		seen := make(map[uint]struct{}, len(sess.Worktrees))
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.WorkspaceID == nil || *wt.WorkspaceID == 0 {
				continue
			}
			id := *wt.WorkspaceID
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
		if len(out) > 0 {
			return out
		}
	}
	out := make([]uint, 0, len(sess.Workspaces))
	for i := range sess.Workspaces {
		if sess.Workspaces[i].WorkspaceID != 0 {
			out = append(out, sess.Workspaces[i].WorkspaceID)
		}
	}
	return out
}

func (s *SessionService) ensureSessionWorktrees(ctx context.Context, sess *Session) {
	wsCache := make(map[uint]*workspace.Workspace)
	if len(sess.Worktrees) > 0 {
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.Branch == nil || wt.Path == "" {
				continue
			}
			ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
			if err != nil {
				slog.WarnContext(ctx, "ensure: workspace lookup failed", "session", sess.ID, "worktree", wt.ID, "error", err)
				continue
			}
			if _, err := os.Stat(wt.Path); err == nil {
				if wt.Status != git.WorktreeStatusPresent {
					wt.Status = git.WorktreeStatusPresent
					if updateErr := s.gitService.UpdateWorktree(wt); updateErr != nil {
						slog.WarnContext(ctx, "failed to mark worktree present", "worktree", wt.ID, "error", updateErr)
					}
				}
				continue
			}
			if err := s.gitService.MarkWorktreeMissing(wt); err != nil {
				slog.WarnContext(ctx, "failed to mark worktree missing", "worktree", wt.ID, "error", err)
			}
			if err := s.gitService.PruneWorktrees(ctx, ws.Path); err != nil {
				slog.WarnContext(ctx, "prune worktrees before ensure failed", "workspace", ws.Name, "error", err)
			}
			repoID := uint(0)
			if ws.RepoID != nil {
				repoID = *ws.RepoID
			}
			if _, _, err := s.gitService.SetupWorktreeAt(ctx, ws.Path, wt.Branch.Name, "", wt.Branch.ID, repoID, wt.Path); err != nil {
				slog.WarnContext(ctx, "failed to ensure session worktree on activation", "session", sess.ID, "workspace", ws.Name, "branch", wt.Branch.Name, "error", err)
				continue
			}
		}
	} else {
		for i := range sess.Workspaces {
			sw := &sess.Workspaces[i]
			if sw.GitBranch == nil || sw.Workspace == nil || sw.WorktreePath == "" {
				continue
			}
			if _, err := os.Stat(sw.WorktreePath); err == nil {
				continue
			}
			if err := s.gitService.PruneWorktrees(ctx, sw.Workspace.Path); err != nil {
				slog.WarnContext(ctx, "prune worktrees before ensure failed", "workspace", sw.Workspace.Name, "error", err)
			}
			repoID := uint(0)
			if sw.Workspace.RepoID != nil {
				repoID = *sw.Workspace.RepoID
			}
			if _, _, err := s.gitService.SetupWorktreeAt(ctx, sw.Workspace.Path, sw.GitBranch.Name, "", sw.GitBranch.ID, repoID, sw.WorktreePath); err != nil {
				slog.WarnContext(ctx, "failed to ensure session worktree on activation", "session", sess.ID, "workspace", sw.Workspace.Name, "branch", sw.GitBranch.Name, "error", err)
				continue
			}
		}
	}
	if sess.IsMulti() && sess.SessionRoot != "" {
		if err := os.MkdirAll(sess.SessionRoot, 0o755); err != nil {
			slog.WarnContext(ctx, "failed to ensure session root", "session", sess.ID, "path", sess.SessionRoot, "error", err)
		}
	}
}

func (s *SessionService) isSessionGitHealthy(ctx context.Context, sess *Session) bool {
	if len(sess.Worktrees) > 0 {
		wsCache := make(map[uint]*workspace.Workspace)
		for i := range sess.Worktrees {
			wt := sess.Worktrees[i].Worktree
			if wt == nil || wt.Branch == nil {
				continue
			}
			ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
			if err != nil {
				return false
			}
			if !s.gitService.IsHealthy(ctx, wt.Branch, ws.Path) {
				return false
			}
		}
		return true
	}
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

	sess := &Session{}

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

	if err := s.CreateSession(ctx, sess, ws.ID, branch.Name, "", reviewAction); err != nil {
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
