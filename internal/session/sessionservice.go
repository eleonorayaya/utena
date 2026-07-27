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

const (
	gitUnhealthyError    = "worktree or branch is unhealthy"
	orphanedSessionError = "session predates multi-workspace migration — please recreate"
)

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
	store                *SessionStore
	sessionWorktreeStore *SessionWorktreeStore
	dismissedPRStore     *DismissedPRStore
	sessionActionStore   *SessionActionStore
	setupStepStore       *SessionSetupStepStore
	workspaceService     *workspace.WorkspaceService
	gitService           *git.GitService
	tmuxService          *utmux.TmuxService
	eventBus             eventbus.EventBus
	branchPrefix         string
	configDir            string
	sessionsRoot         string
	setupTimeout         time.Duration
}

func NewSessionService(store *SessionStore, sessionWorktreeStore *SessionWorktreeStore, dismissedPRStore *DismissedPRStore, sessionActionStore *SessionActionStore, setupStepStore *SessionSetupStepStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, tmuxService *utmux.TmuxService, bus eventbus.EventBus, branchPrefix string, configDir string, sessionsRoot string) *SessionService {
	return &SessionService{
		store:                store,
		sessionWorktreeStore: sessionWorktreeStore,
		dismissedPRStore:     dismissedPRStore,
		sessionActionStore:   sessionActionStore,
		setupStepStore:       setupStepStore,
		workspaceService:     workspaceService,
		gitService:           gitService,
		tmuxService:          tmuxService,
		eventBus:             bus,
		branchPrefix:         branchPrefix,
		configDir:            configDir,
		sessionsRoot:         sessionsRoot,
		setupTimeout:         defaultSetupTimeout,
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
	s.markLegacyLayoutSessionsBroken(sessions)
	s.recoverStuckCreatingSessions(sessions)
	s.reconcileTmuxState(ctx, sessions)
	return nil
}

func (s *SessionService) markLegacyLayoutSessionsBroken(sessions []Session) {
	if s.sessionsRoot == "" {
		return
	}
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusBroken {
			continue
		}
		if sess.SessionRoot == "" {
			continue
		}
		if s.sessionRootWithinRoot(sess.SessionRoot) {
			continue
		}
		if err := s.markSessionBroken(sess, "session uses legacy layout — please recreate"); err != nil {
			slog.Warn("failed to mark legacy-layout session broken", "session", sess.ID, "error", err)
		}
	}
}

func (s *SessionService) markOrphanedSessionsBroken(sessions []Session) {
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusBroken {
			continue
		}
		if len(sess.Worktrees) > 0 {
			continue
		}
		if err := s.markSessionBroken(sess, orphanedSessionError); err != nil {
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

type CreateSessionInput struct {
	Name       string
	Workspaces []WorkspaceBranchSpec
	TodoID     *uint
	Actions    []*SessionAction
}

type assignedSlot struct {
	spec       WorkspaceBranchSpec
	ws         *workspace.Workspace
	destPath   string
	branchName string
}

type worktreeSetupResult struct {
	swt          *SessionWorktree
	wt           *git.Worktree
	ws           *workspace.Workspace
	branch       *git.Branch
	worktreePath string
	created      bool
	warning      string
}

func freeSubdir(base string, used map[string]struct{}) string {
	sub := base
	for suffix := 2; ; suffix++ {
		if _, taken := used[sub]; !taken {
			return sub
		}
		sub = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func (s *SessionService) CreateSession(ctx context.Context, input CreateSessionInput) (*Session, error) {
	if len(input.Workspaces) == 0 {
		return nil, common.NewInvalidRequest("workspaces is required")
	}
	if s.sessionsRoot == "" {
		return nil, common.NewInvalidRequest("sessions root is not configured")
	}

	type slot struct {
		spec WorkspaceBranchSpec
		ws   *workspace.Workspace
	}
	seen := make(map[uint]struct{}, len(input.Workspaces))
	slots := make([]slot, 0, len(input.Workspaces))
	hasBase := false
	for i, spec := range input.Workspaces {
		if spec.WorkspaceID == 0 {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspaces[%d].workspace_id is required", i))
		}
		if _, dup := seen[spec.WorkspaceID]; dup {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %d listed more than once", spec.WorkspaceID))
		}
		seen[spec.WorkspaceID] = struct{}{}
		if (spec.Branch != "") == (spec.BaseBranch != "") {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspaces[%d]: exactly one of branch or base_branch is required", i))
		}
		ws, err := s.workspaceService.GetWorkspace(ctx, spec.WorkspaceID)
		if err != nil {
			return nil, err
		}
		if !ws.IsGitRepo {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name))
		}
		slots = append(slots, slot{spec: spec, ws: ws})
		if spec.BaseBranch != "" {
			hasBase = true
		}
	}

	name := input.Name
	if name == "" {
		if hasBase {
			return nil, common.NewInvalidRequest("name is required when any workspace uses base_branch")
		}
		name = SanitizeSessionName(s.nameFromBranch(slots[0].spec.Branch))
		if name == "" {
			return nil, common.NewInvalidRequest("could not derive session name from branch")
		}
	}
	if err := ValidateSessionName(name); err != nil {
		return nil, common.NewInvalidRequest(err.Error())
	}

	sessionRoot := filepath.Join(s.sessionsRoot, SanitizeSessionName(name))

	for _, sl := range slots {
		if existing, err := s.store.GetByWorkspaceAndName(sl.ws.ID, name, StatusDeleted, StatusArchived, StatusCompleted); err == nil && existing != nil {
			return existing, nil
		}
		_, err := s.store.GetByWorkspaceAndName(sl.ws.ID, name, StatusDeleted, StatusArchived)
		if err == nil {
			return nil, fmt.Errorf("session %q already exists in workspace %q: %w", name, sl.ws.Name, ErrSessionAlreadyExists)
		}
		if !errors.Is(err, ErrSessionNotFound) {
			return nil, fmt.Errorf("failed to check for duplicate session: %w", err)
		}
	}

	used := make(map[string]struct{}, len(slots))
	assigned := make([]assignedSlot, len(slots))
	for i, sl := range slots {
		base := SanitizeSessionName(sl.ws.Name)
		if base == "" {
			base = fmt.Sprintf("workspace-%d", sl.ws.ID)
		}
		sub := freeSubdir(base, used)
		used[sub] = struct{}{}
		branchName := sl.spec.Branch
		if branchName == "" {
			branchName = s.branchPrefix + name
		}
		assigned[i] = assignedSlot{
			spec:       sl.spec,
			ws:         sl.ws,
			destPath:   filepath.Join(sessionRoot, sub),
			branchName: branchName,
		}
	}

	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create session root %q: %w", sessionRoot, err)
	}

	sess := &Session{
		Name:        name,
		TodoID:      input.TodoID,
		Status:      StatusCreating,
		LastUsedAt:  time.Now(),
		SessionRoot: sessionRoot,
	}
	if err := s.store.Add(sess); err != nil {
		slog.WarnContext(ctx, "session create failed; session root preserved", "path", sessionRoot, "error", err)
		return nil, err
	}

	for i, a := range assigned {
		if err := s.eagerCreateWorktree(ctx, sess.ID, a.ws, a.branchName, a.destPath, i); err != nil {
			if delErr := s.store.Delete(sess.ID); delErr != nil {
				slog.ErrorContext(ctx, "failed to delete session after worktree creation failure", "session", sess.ID, "error", delErr)
			}
			slog.WarnContext(ctx, "session worktree create failed; session root preserved", "path", sessionRoot, "workspace", a.ws.ID, "error", err)
			return nil, fmt.Errorf("eager create worktree for workspace %d: %w", a.ws.ID, err)
		}
	}

	for _, action := range input.Actions {
		action.SessionID = sess.ID
		if err := s.sessionActionStore.Add(action); err != nil {
			slog.ErrorContext(ctx, "failed to persist session action", "session", sess.ID, "trigger", action.Trigger, "error", err)
		}
	}

	tmuxName := SanitizeTmuxName(name)
	tmuxEnv := map[string]string{envSessionID: fmt.Sprintf("%d", sess.ID)}
	tmuxRecord, err := s.tmuxService.RegisterPending(tmuxName, sessionRoot, tmuxEnv)
	if err != nil {
		if delErr := s.store.Delete(sess.ID); delErr != nil {
			slog.WarnContext(ctx, "failed to roll back session after tmux registration failure", "session", sess.ID, "error", delErr)
		}
		slog.WarnContext(ctx, "tmux registration failed; session root preserved", "path", sessionRoot, "error", err)
		return nil, fmt.Errorf("register tmux session %q: %w", tmuxName, err)
	}
	sess.TmuxSessionID = &tmuxRecord.ID
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("persist tmux session id: %w", err)
	}

	for _, a := range assigned {
		if err := s.workspaceService.Touch(ctx, a.ws.ID); err != nil {
			slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", a.ws.ID, "error", err)
		}
	}

	stepWS := assignedToStepWorkspaces(assigned)
	defs := buildSetupSteps(stepWS, s.loadWorkspaceConfigs(stepWS))
	if _, err := s.persistSetupSteps(sess.ID, defs); err != nil {
		slog.WarnContext(ctx, "failed to pre-create setup steps", "session", sess.ID, "error", err)
	}

	go s.runSetup(sess.ID, tmuxName, input.Workspaces)

	return sess, nil
}

func (s *SessionService) resolveRepoID(ctx context.Context, ws *workspace.Workspace) (uint, error) {
	if ws.RepoID != nil {
		return *ws.RepoID, nil
	}
	repo, err := s.gitService.FindOrCreateRepo(ctx, ws.Path)
	if err != nil {
		return 0, fmt.Errorf("workspace %q has no git repo and lookup failed: %w", ws.Name, err)
	}
	return repo.ID, nil
}

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
	wt, err := s.gitService.RegisterPendingWorktree(branch.ID, repoID, worktreePath)
	if err != nil {
		return fmt.Errorf("register pending worktree: %w", err)
	}
	swt := &SessionWorktree{
		SessionID:  sessionID,
		WorktreeID: wt.ID,
		Position:   position,
	}
	if err := s.sessionWorktreeStore.Add(swt); err != nil {
		// duplicate junction rows are expected on repair / re-entry — surface
		// only unexpected failures
		if !errors.Is(err, ErrSessionWorktreeAlreadyExists) {
			return fmt.Errorf("add session-worktree junction: %w", err)
		}
	}
	return nil
}

func (s *SessionService) AddWorkspace(ctx context.Context, sessionID uint, spec WorkspaceBranchSpec) (*Session, error) {
	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		return nil, err
	}

	if sess.Status == StatusDeleted || sess.Status == StatusArchived {
		return nil, common.NewInvalidRequest("cannot add workspace to deleted or archived session")
	}

	ws, err := s.workspaceService.GetWorkspace(ctx, spec.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !ws.IsGitRepo {
		return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository", ws.Name))
	}

	repoID, err := s.resolveRepoID(ctx, ws)
	if err != nil {
		return nil, err
	}
	used := make(map[string]struct{}, len(sess.Worktrees))
	for i := range sess.Worktrees {
		wt := sess.Worktrees[i].Worktree
		if wt == nil {
			continue
		}
		if wt.RepoID == repoID {
			return nil, common.NewConflict(fmt.Sprintf("workspace %q is already in this session", ws.Name))
		}
		if wt.Path != "" {
			used[filepath.Base(wt.Path)] = struct{}{}
		}
	}
	base := SanitizeSessionName(ws.Name)
	if base == "" {
		base = fmt.Sprintf("workspace-%d", ws.ID)
	}
	destPath := filepath.Join(sess.SessionRoot, freeSubdir(base, used))

	branchName := spec.Branch
	if branchName == "" {
		branchName = s.branchPrefix + sess.Name
	}

	position := len(sess.Worktrees)
	if err := s.eagerCreateWorktree(ctx, sess.ID, ws, branchName, destPath, position); err != nil {
		return nil, fmt.Errorf("eager create worktree: %w", err)
	}

	if err := s.workspaceService.Touch(ctx, ws.ID); err != nil {
		slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", ws.ID, "error", err)
	}

	go s.runAddWorkspace(sess.ID, spec)

	return s.store.GetByID(sess.ID)
}

func (s *SessionService) runAddWorkspace(sessionID uint, spec WorkspaceBranchSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runAddWorkspace: failed to load session", "id", sessionID, "error", err)
		return
	}

	ctx = slogctx.Append(ctx,
		"session", sess.ID,
		"session_root", sess.SessionRoot,
		"workspace_count", len(sess.Worktrees),
	)

	markBroken := func(stage string, err error) {
		msg := fmt.Sprintf("%s failed: %v", stage, err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("%s timed out after %s", stage, s.setupTimeout)
		}
		if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
		}
	}

	if len(sess.Worktrees) == 0 {
		markBroken("find new worktree", fmt.Errorf("session %d has no worktrees", sess.ID))
		return
	}
	newSwt := &sess.Worktrees[len(sess.Worktrees)-1]

	ws := newSwt.Workspace
	if ws == nil {
		markBroken("workspace lookup", fmt.Errorf("worktree %d has no workspace", newSwt.WorktreeID))
		return
	}

	var setupWarnings []string
	result, err := s.setupSingleWorktree(ctx, sess, newSwt, ws, spec)
	if err != nil {
		markBroken("worktree setup", err)
		return
	}
	if result.warning != "" {
		setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", ws.Name, result.warning))
	}

	if result.created {
		branchName := ""
		if result.branch != nil {
			branchName = result.branch.Name
		}
		env := []string{
			envSessionRoot + "=" + sess.SessionRoot,
			envSessionName + "=" + sess.Name,
			envBranch + "=" + branchName,
			envWorktreePath + "=" + result.worktreePath,
			envWorkspaceName + "=" + ws.Name,
			envWorkspacePath + "=" + ws.Path,
		}
		wsScript := filepath.Join(ws.Path, ".utena", worktreeSetupName)
		if err := traceOp(ctx, "worktree-setup script", func() error {
			return s.runScript(ctx, wsScript, result.worktreePath, env)
		}, "script", wsScript, "ws", ws.Name); err != nil {
			slog.WarnContext(ctx, "workspace worktree-setup script failed", "ws", ws.Name, "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", ws.Name, err.Error()))
		}

		globalEnv := []string{
			envSessionRoot + "=" + sess.SessionRoot,
			envSessionName + "=" + sess.Name,
			envBranch + "=" + branchName,
		}
		globalScript := filepath.Join(s.configDir, worktreeSetupName)
		if err := traceOp(ctx, "global worktree-setup script", func() error {
			return s.runScript(ctx, globalScript, sess.SessionRoot, globalEnv)
		}, "script", globalScript); err != nil {
			slog.WarnContext(ctx, "global worktree-setup script failed", "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("global: %s", err.Error()))
		}
	}

	setupWarnings = append(setupWarnings, s.applyClaudeSettings(ctx, sess)...)

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

func (s *SessionService) applyClaudeSettings(ctx context.Context, sess *Session) []string {
	var warnings []string
	workspacePaths := make([]string, 0, len(sess.Worktrees))
	checkouts := make([]claudesettings.SessionCheckout, 0, len(sess.Worktrees))
	for i := range sess.Worktrees {
		swt := &sess.Worktrees[i]
		wt := swt.Worktree
		if wt == nil || wt.Path == "" {
			continue
		}
		ws := swt.Workspace
		if ws == nil {
			slog.WarnContext(ctx, "claude settings: workspace not populated", "worktree", wt.ID)
			continue
		}
		workspacePaths = append(workspacePaths, ws.Path)
		checkouts = append(checkouts, claudesettings.SessionCheckout{
			Subdir:        filepath.Base(wt.Path),
			WorkspaceName: ws.Name,
		})
	}
	if err := claudesettings.EnsureSessionRoot(sess.SessionRoot, workspacePaths); err != nil {
		slog.WarnContext(ctx, "ensure session-root claude settings failed", "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-settings: %s", err.Error()))
	}
	if err := claudesettings.EnsureMultiSessionGuide(sess.SessionRoot, checkouts); err != nil {
		slog.WarnContext(ctx, "ensure session CLAUDE.md failed", "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-guide: %s", err.Error()))
	}
	return warnings
}

func (s *SessionService) resolveWorktreeWorkspace(ctx context.Context, wt *git.Worktree, cache map[uint]*workspace.Workspace) (*workspace.Workspace, error) {
	if wt == nil {
		return nil, fmt.Errorf("worktree is nil")
	}
	if wt.RepoID == 0 {
		return nil, fmt.Errorf("worktree %d has no repo", wt.ID)
	}
	if ws, ok := cache[wt.RepoID]; ok {
		return ws, nil
	}
	ws, err := s.workspaceService.GetWorkspaceByRepoID(ctx, wt.RepoID)
	if err != nil {
		return nil, err
	}
	cache[wt.RepoID] = ws
	return ws, nil
}

func (s *SessionService) setupSingleWorktree(ctx context.Context, sess *Session, swt *SessionWorktree, ws *workspace.Workspace, spec WorkspaceBranchSpec) (worktreeSetupResult, error) {
	wt := swt.Worktree
	if wt == nil {
		return worktreeSetupResult{}, fmt.Errorf("session worktree %d has nil worktree ref", swt.ID)
	}

	baseBranchName := spec.BaseBranch
	branchName := spec.Branch
	finalBranchName := branchName
	if baseBranchName != "" {
		finalBranchName = s.branchPrefix + sess.Name
	}
	if finalBranchName == "" && wt.Branch != nil {
		finalBranchName = wt.Branch.Name
	}

	var warning string
	if err := s.setupBranch(ctx, ws, branchName, baseBranchName); err != nil {
		var w SetupWarning
		if !errors.As(err, &w) {
			return worktreeSetupResult{}, fmt.Errorf("branch setup: %w", err)
		}
		warning = w.Message
	}

	repoID, err := s.resolveRepoID(ctx, ws)
	if err != nil {
		return worktreeSetupResult{}, fmt.Errorf("repo lookup: %w", err)
	}

	branch, err := tracedOp(ctx, "find-or-create-branch", func() (*git.Branch, error) {
		return s.gitService.FindOrCreateBranch(ctx, finalBranchName, repoID)
	}, "name", finalBranchName, "ws", ws.Name)
	if err != nil {
		return worktreeSetupResult{}, fmt.Errorf("find-or-create-branch: %w", err)
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
		return worktreeSetupResult{}, fmt.Errorf("worktree setup: %w", err)
	}

	if created && baseBranchName != "" {
		if pushErr := s.gitService.PushSetUpstream(ctx, wtPath, finalBranchName); pushErr != nil {
			slog.WarnContext(ctx, "push --set-upstream failed", "branch", finalBranchName, "error", pushErr)
			if warning == "" {
				warning = fmt.Sprintf("upstream not set: %v", pushErr)
			}
		}
	}

	if refreshed, err := s.gitService.GetWorktree(wt.ID); err == nil {
		*wt = *refreshed
	}
	if wt.Path == "" {
		wt.Path = wtPath
	}

	return worktreeSetupResult{swt: swt, wt: wt, ws: ws, branch: branch, worktreePath: wt.Path, created: created, warning: warning}, nil
}

func (s *SessionService) runSetup(sessionID uint, tmuxName string, specs []WorkspaceBranchSpec) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	specByWS := make(map[uint]WorkspaceBranchSpec, len(specs))
	for _, sp := range specs {
		specByWS[sp.WorkspaceID] = sp
	}

	ctx = slogctx.Append(ctx,
		"session", sess.ID,
		"session_root", sess.SessionRoot,
		"workspace_count", len(sess.Worktrees),
	)
	slog.InfoContext(ctx, "session setup: start", "tmux", tmuxName, "timeout", s.setupTimeout)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "session setup: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
	}()

	markBroken := func(stage string, err error) {
		msg := fmt.Sprintf("%s failed: %v", stage, err)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			msg = fmt.Sprintf("%s timed out after %s", stage, s.setupTimeout)
		}
		if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
		}
	}

	if len(sess.Worktrees) == 0 {
		markBroken("missing worktrees", fmt.Errorf("session %d has no SessionWorktree rows", sess.ID))
		return
	}

	wsCache := make(map[uint]*workspace.Workspace)
	orderedWS := make([]stepWorkspace, 0, len(sess.Worktrees))
	wsByID := make(map[uint]*workspace.Workspace, len(sess.Worktrees))
	swtByID := make(map[uint]*SessionWorktree, len(sess.Worktrees))
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
		orderedWS = append(orderedWS, stepWorkspace{ID: ws.ID, Name: ws.Name, Path: ws.Path})
		wsByID[ws.ID] = ws
		swtByID[ws.ID] = swt
	}

	configs := s.loadWorkspaceConfigs(orderedWS)
	defs := buildSetupSteps(orderedWS, configs)
	steps := s.ensureSetupSteps(sess.ID, defs)

	execSteps := make([]execStep, len(defs))
	for i := range defs {
		execSteps[i] = execStep{def: defs[i], db: steps[i]}
	}

	executor := &setupExecution{
		svc:       s,
		sess:      sess,
		tmuxName:  tmuxName,
		execSteps: execSteps,
		orderedWS: orderedWS,
		specByWS:  specByWS,
		wsByID:    wsByID,
		swtByID:   swtByID,
		updater:   &stepUpdater{store: s.setupStepStore},
		results:   make(map[uint]*worktreeSetupResult, len(orderedWS)),
	}
	executor.run(ctx)

	if executor.broken {
		markBroken(executor.brokenStage, executor.brokenErr)
		return
	}
	if ctx.Err() != nil {
		markBroken("setup", ctx.Err())
		return
	}

	sess.Status = StatusActive
	if len(executor.warnings) == 0 {
		sess.StatusError = ""
	} else {
		sess.StatusError = strings.Join(executor.warnings, "; ")
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

	if len(sess.Worktrees) == 0 {
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

	wsCache := make(map[uint]*workspace.Workspace)
	specs := make([]WorkspaceBranchSpec, 0, len(sess.Worktrees))
	for i := range sess.Worktrees {
		wt := sess.Worktrees[i].Worktree
		if wt == nil || wt.Branch == nil {
			continue
		}
		ws, err := s.resolveWorktreeWorkspace(context.Background(), wt, wsCache)
		if err != nil {
			continue
		}
		specs = append(specs, WorkspaceBranchSpec{WorkspaceID: ws.ID, Branch: wt.Branch.Name})
	}
	go s.runSetup(sess.ID, tmuxName, specs)
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

	tmuxName := SanitizeTmuxName(session.Name)
	if session.TmuxSessionID != nil && session.TmuxSession != nil && session.TmuxSession.Name != "" {
		tmuxName = session.TmuxSession.Name
	}
	if tmuxName == "" {
		return nil, fmt.Errorf("session has no tmux record; please repair")
	}

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
		session.TmuxSession = ts
		session.Status = StatusActive
		session.StatusError = ""
	}

	if session.IsAttached {
		slog.Info("skipping activation for already attached session", "session", id)
		return session, nil
	}

	if err := s.store.ClearAttachedExcept(session.ID); err != nil {
		slog.Warn("failed to clear stale attached flags", "error", err)
	}

	session.LastUsedAt = time.Now()
	session.IsAttached = true

	if err := s.store.Update(session); err != nil {
		return nil, err
	}

	wsCache := make(map[uint]*workspace.Workspace)
	for i := range session.Worktrees {
		wt := session.Worktrees[i].Worktree
		if wt == nil {
			continue
		}
		ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
		if err != nil {
			continue
		}
		if err := s.workspaceService.Touch(ctx, ws.ID); err != nil {
			slog.Warn("failed to touch workspace last-used timestamp", "workspace", ws.ID, "error", err)
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

	if !force {
		if session.IsCreating() {
			return common.NewInvalidRequest("cannot delete session while it is being created")
		}
		if !session.CanDelete() {
			return ErrSessionNotArchived
		}
	}

	s.cleanupSessionResources(ctx, session, deleteBranch)

	if err := s.store.HardDelete(id); err != nil {
		return err
	}

	for i := range session.Worktrees {
		wt := session.Worktrees[i].Worktree
		if wt == nil {
			continue
		}
		if err := s.gitService.DeleteWorktree(wt.ID); err != nil {
			slog.Warn("failed to delete worktree record", "worktree", wt.ID, "error", err)
		}
	}

	return nil
}

// cleanupSessionResources tears down every on-disk and tmux resource a session
// owns, continuing past individual failures so a single stuck resource doesn't
// strand the rest of the cleanup.
func (s *SessionService) cleanupSessionResources(ctx context.Context, session *Session, deleteBranch bool) {
	if session.TmuxSessionID != nil {
		if err := s.tmuxService.KillSession(*session.TmuxSessionID); err != nil {
			slog.Warn("failed to kill tmux session", "error", err)
		}
		session.TmuxSessionID = nil
	}

	wsCache := make(map[uint]*workspace.Workspace)
	for i := range session.Worktrees {
		wt := session.Worktrees[i].Worktree
		if wt == nil {
			continue
		}
		ws, err := s.resolveWorktreeWorkspace(ctx, wt, wsCache)
		if err != nil {
			slog.Warn("cleanup: failed to resolve workspace", "worktree", wt.ID, "error", err)
			continue
		}
		if wt.Path != "" {
			if err := s.gitService.RemoveWorktree(ctx, ws.Path, wt.Path); err != nil {
				slog.Warn("failed to remove worktree dir", "worktree", wt.ID, "error", err)
			}
		}
		if wt.Branch != nil {
			if err := s.gitService.CleanupBranch(ctx, wt.Branch, ws.Path, deleteBranch); err != nil {
				slog.Warn("failed to cleanup branch", "workspace_id", ws.ID, "error", err)
			}
		}
	}

	s.removeSessionRoot(session)
}

// removeSessionRoot deletes the session's container directory, but only when it
// is safely nested under the configured sessions root, to avoid touching
// arbitrary paths recorded on legacy sessions.
func (s *SessionService) removeSessionRoot(session *Session) {
	if !s.sessionRootWithinRoot(session.SessionRoot) {
		return
	}
	if err := os.RemoveAll(session.SessionRoot); err != nil {
		slog.Warn("failed to remove session root dir", "path", session.SessionRoot, "error", err)
	}
}

// sessionRootWithinRoot reports whether sessionRoot is nested under the
// configured sessions root, the invariant that distinguishes container-layout
// sessions from legacy ones with arbitrary paths.
func (s *SessionService) sessionRootWithinRoot(sessionRoot string) bool {
	if s.sessionsRoot == "" || sessionRoot == "" {
		return false
	}
	return strings.HasPrefix(sessionRoot, s.sessionsRoot+string(filepath.Separator))
}

// DetachWorktreesByRepoID drops join rows first so the worktree delete
// doesn't trip the RESTRICT FK on session_worktrees.worktree_id. Sessions
// left with no remaining worktrees are kept for the user to clean up.
func (s *SessionService) DetachWorktreesByRepoID(ctx context.Context, repoID uint) error {
	if err := s.sessionWorktreeStore.HardDeleteByWorktreeRepoID(repoID); err != nil {
		return fmt.Errorf("failed to detach session worktrees for repo %d: %w", repoID, err)
	}
	if err := s.gitService.DeleteWorktreesByRepoID(repoID); err != nil {
		return fmt.Errorf("failed to delete worktrees for repo %d: %w", repoID, err)
	}
	return nil
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
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted {
			continue
		}
		if _, err := s.reconcileLoadedSession(ctx, sess); err != nil {
			slog.Warn("failed to refresh session during reconcile", "session", sess.ID, "error", err)
		}
	}
}

func (s *SessionService) ArchiveSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if !sess.CanArchive() {
		return nil, fmt.Errorf("cannot archive session in status %s", sess.Status)
	}

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
	return s.reconcileLoadedSession(ctx, sess)
}

func (s *SessionService) reconcileLoadedSession(ctx context.Context, sess *Session) (*Session, error) {
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

	prevStatus := sess.Status
	prevError := sess.StatusError
	canRecoverFromBroken := sess.Status == StatusBroken && sess.StatusError == gitUnhealthyError

	if !gitHealthy {
		sess.Status = StatusBroken
		sess.StatusError = gitUnhealthyError
	} else if !tmuxAlive {
		if sess.Status == StatusActive || canRecoverFromBroken {
			sess.Status = StatusInactive
			sess.StatusError = ""
		}
	} else {
		if sess.Status == StatusInactive || canRecoverFromBroken {
			sess.Status = StatusActive
			sess.StatusError = ""
		}
	}

	if sess.Status == prevStatus && sess.StatusError == prevError {
		return sess, nil
	}
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("failed to persist reconciled session: %w", err)
	}
	return sess, nil
}

func (s *SessionService) sessionBranchIDs(sess *Session) []uint {
	out := make([]uint, 0, len(sess.Worktrees))
	for i := range sess.Worktrees {
		wt := sess.Worktrees[i].Worktree
		if wt == nil || wt.BranchID == 0 {
			continue
		}
		out = append(out, wt.BranchID)
	}
	return out
}

func (s *SessionService) ensureSessionWorktrees(ctx context.Context, sess *Session) {
	wsCache := make(map[uint]*workspace.Workspace)
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
	if sess.SessionRoot != "" {
		if err := os.MkdirAll(sess.SessionRoot, 0o755); err != nil {
			slog.WarnContext(ctx, "failed to ensure session root", "session", sess.ID, "path", sess.SessionRoot, "error", err)
		}
	}
}

func (s *SessionService) isSessionGitHealthy(ctx context.Context, sess *Session) bool {
	wsCache := make(map[uint]*workspace.Workspace)
	for i := range sess.Worktrees {
		wt := sess.Worktrees[i].Worktree
		if wt == nil || wt.Branch == nil {
			continue
		}
		// Worktrees not yet created on disk (pending) or already known to be
		// missing aren't considered unhealthy here — they represent
		// intentional intermediate states.
		if wt.Status != git.WorktreeStatusPresent {
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

	s.notifyPRUpdated(ctx, data)

	return nil
}

func (s *SessionService) notifyPRUpdated(ctx context.Context, data git.PRUpdatedEvent) {
	sess, err := s.store.GetByBranchID(*data.PullRequest.HeadBranchID)
	if err != nil {
		return
	}

	event := eventbus.Event{
		Type: eventbus.SessionNotification,
		Data: eventbus.SessionNotificationEvent{
			SessionID: sess.ID,
			Type:      notificationTypePullRequest,
			Data:      s.newPRNotification(data.PullRequest, data.Previous),
		},
	}
	if err := s.eventBus.Publish(ctx, event); err != nil {
		slog.Warn("failed to publish session notification", "session", sess.ID, "pr", data.PullRequest.Number, "error", err)
	}
}

func (s *SessionService) SessionSnapshot(ctx context.Context, sessionID uint) []eventbus.SessionNotificationEvent {
	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		return nil
	}

	var out []eventbus.SessionNotificationEvent
	for _, branchID := range s.sessionBranchIDs(sess) {
		for _, pr := range s.gitService.GetPRsForBranch(branchID) {
			if pr.State != git.PRStateOpen && pr.State != git.PRStateDraft {
				continue
			}
			out = append(out, eventbus.SessionNotificationEvent{
				SessionID: sessionID,
				Type:      notificationTypePullRequest,
				Data:      s.newPRNotification(&pr, nil),
			})
		}
	}
	return out
}

func (s *SessionService) newPRNotification(pr *git.PullRequest, previous *git.PullRequest) prNotification {
	n := prNotification{
		Number: pr.Number,
		Title:  pr.Title,
		State:  string(pr.State),
		URL:    pr.HTMLURL,
	}
	if previous != nil && previous.State != pr.State {
		n.PreviousState = string(previous.State)
	}
	if pr.HeadBranchID != nil {
		if branch, err := s.gitService.GetBranch(*pr.HeadBranchID); err == nil {
			n.Branch = branch.Name
		}
	}
	return n
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

	prompt := fmt.Sprintf("/git-workflows:better-review %s", pr.HTMLURL)
	reviewAction := &SessionAction{
		Trigger: TriggerOnCreate,
		Type:    SessionActionTypeClaude,
		Options: marshalOptions(ClaudeActionOptions{Prompt: prompt}),
	}

	sess, err := s.CreateSession(ctx, CreateSessionInput{
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: ws.ID, Branch: branch.Name}},
		Actions:    []*SessionAction{reviewAction},
	})
	if err != nil {
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
