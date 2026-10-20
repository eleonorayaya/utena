package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/eleonorayaya/utena/internal/workspace"
	slogctx "github.com/veqryn/slog-context"
)

type SetupWarning struct{ Message string }

func (w SetupWarning) Error() string { return w.Message }

const (
	EnvSessionID      = "UTENA_SESSION_ID"
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
	sessionBranchStore   *SessionBranchStore
	dismissedPRStore     *DismissedPRStore
	sessionActionStore   *SessionActionStore
	setupStepStore       *SessionSetupStepStore
	blockerStore         *SessionBlockerStore
	workspaceService     *workspace.WorkspaceService
	gitService           *git.GitService
	muxService           *mux.Service
	eventBus             eventbus.EventBus
	branchPrefix         string
	configDir            string
	sources              workspace.SourceReader
	sessionsRoot         string
	setupTimeout         time.Duration
	bundleCatalog        bundles.Catalog
	mcpServers           bundles.Servers
	claudeGlobalConfig   string
	trustedMu            sync.Mutex
	trustedRoots         map[string]struct{}
	remote               RemoteBackend
}

func NewSessionService(store *SessionStore, sessionWorktreeStore *SessionWorktreeStore, dismissedPRStore *DismissedPRStore, sessionActionStore *SessionActionStore, setupStepStore *SessionSetupStepStore, workspaceService *workspace.WorkspaceService, gitService *git.GitService, muxService *mux.Service, bus eventbus.EventBus, branchPrefix string, configDir string, sessionsRoot string) *SessionService {
	return &SessionService{
		store:                store,
		sessionWorktreeStore: sessionWorktreeStore,
		sessionBranchStore:   NewSessionBranchStore(store.db),
		dismissedPRStore:     dismissedPRStore,
		sessionActionStore:   sessionActionStore,
		setupStepStore:       setupStepStore,
		blockerStore:         NewSessionBlockerStore(store.db),
		workspaceService:     workspaceService,
		gitService:           gitService,
		muxService:           muxService,
		eventBus:             bus,
		branchPrefix:         branchPrefix,
		configDir:            configDir,
		sources:              workspace.NewLocalSourceReader(configDir),
		sessionsRoot:         sessionsRoot,
		setupTimeout:         defaultSetupTimeout,
	}
}

func (s *SessionService) SetBundleCatalog(catalog bundles.Catalog, servers bundles.Servers) {
	s.bundleCatalog = catalog
	s.mcpServers = servers
}

func (s *SessionService) SetSourceReader(reader workspace.SourceReader) {
	s.sources = reader
}

func (s *SessionService) SetClaudeGlobalConfigPath(path string) {
	s.trustedMu.Lock()
	defer s.trustedMu.Unlock()
	s.claudeGlobalConfig = path
	s.trustedRoots = nil
}

func (s *SessionService) trustSessionRoot(sess *Session) error {
	root := sess.SessionRoot
	s.trustedMu.Lock()
	path := s.claudeGlobalConfig
	_, trusted := s.trustedRoots[root]
	s.trustedMu.Unlock()
	if root == "" || path == "" || trusted {
		return nil
	}
	if err := claudesettings.TrustProject(path, root); err != nil {
		return err
	}
	s.trustedMu.Lock()
	defer s.trustedMu.Unlock()
	if s.trustedRoots == nil {
		s.trustedRoots = make(map[string]struct{})
	}
	s.trustedRoots[root] = struct{}{}
	return nil
}

func (s *SessionService) OnAppStart(ctx context.Context) error {
	s.eventBus.Subscribe(eventbus.MuxSessionCreated, s.handleMuxSessionCreated)
	s.eventBus.Subscribe(eventbus.MuxSessionClosed, s.handleMuxSessionClosed)
	s.eventBus.Subscribe(eventbus.MuxClientSessionChanged, s.handleMuxClientSessionChanged)
	s.eventBus.Subscribe(eventbus.MuxClientAttached, s.handleMuxClientAttached)
	s.eventBus.Subscribe(eventbus.MuxClientDetached, s.handleMuxClientDetached)
	s.eventBus.Subscribe(git.EventPRUpdated, s.handlePRUpdated)
	sessions, err := s.store.List()
	if err != nil {
		slog.Warn("startup: failed to list sessions", "error", err)
		return nil
	}
	s.markOrphanedSessionsBroken(sessions)
	s.markLegacyLayoutSessionsBroken(sessions)
	s.recoverStuckCreatingSessions(sessions)
	s.resumeCoderDeletes(sessions)
	s.reconcileMuxState(ctx, sessions)
	s.syncWatchedSessionBranches(ctx, sessions)
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
		if len(sess.Worktrees) > 0 || sess.IsCoder() {
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

func (s *SessionService) populateMuxWindows(ctx context.Context, sess *Session) {
	if sess.MuxSession != nil {
		sess.MuxSession.Windows = s.muxService.GetWindows(ctx, sess.MuxSession.Name)
	}
}

func (s *SessionService) ListSessions(ctx context.Context, statuses ...SessionStatus) ([]Session, error) {
	sessions, err := s.store.List(statuses...)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		s.populateMuxWindows(ctx, &sessions[i])
	}
	s.attachPullRequests(sessions)
	return sessions, nil
}

func (s *SessionService) ResourceUsage(ctx context.Context) (*SessionResourcesResponse, error) {
	usages, err := s.muxService.ResourceUsage(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(usages))
	for i, u := range usages {
		names[i] = u.Session
	}
	refs, err := s.store.ListByMuxNames(names)
	if err != nil {
		return nil, err
	}
	byMux := make(map[string]MuxSessionRef, len(refs))
	for _, ref := range refs {
		byMux[ref.MuxName] = ref
	}

	rows := make([]SessionResources, 0, len(usages))
	for _, u := range usages {
		row := SessionResources{
			Name:       u.Session,
			MuxSession: u.Session,
			Status:     ResourceStatusUnmanaged,
			CPUPercent: u.CPUPercent,
			RSSBytes:   u.RSSBytes,
			Procs:      u.Procs,
			Processes:  u.Processes,
		}
		if ref, ok := byMux[u.Session]; ok {
			row.SessionID = &ref.ID
			row.Name = ref.Name
			row.Status = string(ref.Status)
		}
		rows = append(rows, row)
	}
	return &SessionResourcesResponse{Sessions: rows, SampledAt: time.Now()}, nil
}

func (s *SessionService) attachPullRequests(sessions []Session) {
	var branchIDs []uint
	for i := range sessions {
		branchIDs = append(branchIDs, sessions[i].BranchIDs()...)
	}
	byBranch := groupPRsByBranch(s.gitService.GetPRsForBranches(branchIDs))
	for i := range sessions {
		if sessions[i].IsCoder() {
			sessions[i].PullRequests = sessions[i].RemotePRs
			continue
		}
		var prs []git.PullRequest
		for _, id := range sessions[i].BranchIDs() {
			prs = append(prs, byBranch[id]...)
		}
		sessions[i].PullRequests = prs
	}
}

func groupPRsByBranch(prs []git.PullRequest) map[uint][]git.PullRequest {
	byBranch := make(map[uint][]git.PullRequest)
	for _, pr := range prs {
		if pr.HeadBranchID != nil {
			byBranch[*pr.HeadBranchID] = append(byBranch[*pr.HeadBranchID], pr)
		}
	}
	return byBranch
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
		s.populateMuxWindows(ctx, &sessions[i])
	}
	s.attachPullRequests(sessions)
	return sessions, nil
}

func (s *SessionService) AttachedSessionID(ctx context.Context) (uint, error) {
	return s.store.AttachedID()
}

func (s *SessionService) GetSession(ctx context.Context, id uint) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	s.populateMuxWindows(ctx, sess)
	if sess.IsCoder() {
		sess.PullRequests = sess.RemotePRs
		return sess, nil
	}
	sess.PullRequests = s.gitService.GetPRsForBranches(sess.BranchIDs())
	return sess, nil
}

func (s *SessionService) IsAttached(id uint) (bool, error) {
	return s.store.IsAttached(id)
}

func (s *SessionService) LeaderSessionID(id uint) (*uint, error) {
	return s.store.LeaderSessionID(id)
}

func (s *SessionService) findSessionByMuxName(muxName string) (*Session, error) {
	ts, err := s.muxService.GetSessionByName(muxName)
	if err != nil {
		return nil, err
	}
	return s.store.GetByMuxSessionID(ts.ID)
}

type CreateSessionInput struct {
	Name            string
	Workspaces      []WorkspaceBranchSpec
	Actions         []*SessionAction
	Bundles         []string
	LeaderSessionID *uint
	Backend         Backend
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

	switch input.Backend {
	case "", BackendLocal:
	case BackendCoder:
		if len(slots) != 1 {
			return nil, common.NewInvalidRequest("coder sessions take exactly one workspace")
		}
		return s.createCoderSession(ctx, name, assignedSlot{spec: slots[0].spec, ws: slots[0].ws}, input)
	default:
		return nil, common.NewInvalidRequest(fmt.Sprintf("unknown backend %q: must be local or coder", input.Backend))
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

	stepWS := assignedToStepWorkspaces(assigned)
	configs, err := s.loadWorkspaceConfigs(ctx, stepWS)
	if err != nil {
		return nil, fmt.Errorf("load workspace config: %w", err)
	}
	sessBundles := input.Bundles
	if sessBundles == nil {
		sessBundles = defaultBundlesFrom(stepWS, configs)
	}

	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create session root %q: %w", sessionRoot, err)
	}

	sess := &Session{
		Name:            name,
		Status:          StatusCreating,
		LastUsedAt:      time.Now(),
		SessionRoot:     sessionRoot,
		Bundles:         bundles.Union(sessBundles),
		LeaderSessionID: input.LeaderSessionID,
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

	muxName := SanitizeMuxName(name)
	muxEnv := map[string]string{EnvSessionID: fmt.Sprintf("%d", sess.ID)}
	muxRecord, err := s.muxService.RegisterPending(muxName, sessionRoot, muxEnv)
	if err != nil {
		if delErr := s.store.Delete(sess.ID); delErr != nil {
			slog.WarnContext(ctx, "failed to roll back session after mux registration failure", "session", sess.ID, "error", delErr)
		}
		slog.WarnContext(ctx, "mux registration failed; session root preserved", "path", sessionRoot, "error", err)
		return nil, fmt.Errorf("register mux session %q: %w", muxName, err)
	}
	sess.MuxSessionID = &muxRecord.ID
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("persist mux session id: %w", err)
	}

	for _, a := range assigned {
		if err := s.workspaceService.Touch(ctx, a.ws.ID); err != nil {
			slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", a.ws.ID, "error", err)
		}
	}

	defs := buildSetupSteps(stepWS, configs)
	if _, err := s.persistSetupSteps(sess.ID, defs); err != nil {
		slog.WarnContext(ctx, "failed to pre-create setup steps", "session", sess.ID, "error", err)
	}

	go s.runSetup(sess.ID, muxName, input.Workspaces)

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
	if sess.IsCoder() {
		return nil, ErrCoderUnsupported
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

	wsBundles, err := defaultBundles(ctx, s.sources, []workspace.Workspace{*ws})
	if err != nil {
		return nil, fmt.Errorf("load workspace config: %w", err)
	}

	position := len(sess.Worktrees)
	if err := s.eagerCreateWorktree(ctx, sess.ID, ws, branchName, destPath, position); err != nil {
		return nil, fmt.Errorf("eager create worktree: %w", err)
	}

	if err := s.workspaceService.Touch(ctx, ws.ID); err != nil {
		slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", ws.ID, "error", err)
	}

	if merged := bundles.Union(sess.Bundles, wsBundles); len(merged) != len(sess.Bundles) {
		if err := s.store.UpdateBundles(sess.ID, merged); err != nil {
			slog.WarnContext(ctx, "failed to persist session bundles", "session", sess.ID, "error", err)
		}
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

	markBroken := func(stage string, err error) { s.markSetupBroken(ctx, sess, stage, err) }

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

	orderedWS := []stepWorkspace{newStepWorkspace(ws)}
	configs, err := s.loadWorkspaceConfigs(ctx, orderedWS)
	if err != nil {
		markBroken("load workspace config", err)
		return
	}
	defs := buildWorkspaceSetupSteps(orderedWS, configs)
	executor := s.newSetupExecution(sess, "", defs, orderedWS,
		map[uint]WorkspaceBranchSpec{ws.ID: spec},
		map[uint]*workspace.Workspace{ws.ID: ws},
		map[uint]*SessionWorktree{ws.ID: newSwt},
	)
	s.completeSetup(ctx, sess, executor)
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
	if current, err := s.store.GetBundles(sess.ID); err != nil {
		slog.WarnContext(ctx, "failed to load session bundles", "session", sess.ID, "error", err)
	} else {
		sess.Bundles = current
	}
	warnings = append(warnings, s.applyClaudeBundles(ctx, sess)...)
	if err := claudesettings.EnsureMultiSessionGuide(sess.SessionRoot, checkouts); err != nil {
		slog.WarnContext(ctx, "ensure session CLAUDE.md failed", "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-guide: %s", err.Error()))
	}
	return warnings
}

func (s *SessionService) applyClaudeBundles(ctx context.Context, sess *Session) []string {
	var warnings []string
	if err := s.writeBundles(ctx, sess.SessionRoot, sess.Bundles); err != nil {
		slog.WarnContext(ctx, "apply claude bundles failed", "session", sess.ID, "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-bundles: %s", err.Error()))
	}
	if err := s.trustSessionRoot(sess); err != nil {
		slog.WarnContext(ctx, "trust session root in claude config failed", "session", sess.ID, "error", err)
		warnings = append(warnings, fmt.Sprintf("claude-trust: %s", err.Error()))
	}
	return warnings
}

func (s *SessionService) SetBundles(ctx context.Context, id uint, names []string) (*Session, error) {
	sess, err := s.store.GetByID(id)
	if err != nil {
		return nil, err
	}
	if sess.Status == StatusDeleted {
		return nil, common.NewInvalidRequest("cannot set bundles on a deleted session")
	}
	if sess.IsCoder() {
		return nil, ErrCoderUnsupported
	}
	sess.Bundles = bundles.Union(names)
	if err := s.store.UpdateBundles(sess.ID, sess.Bundles); err != nil {
		return nil, fmt.Errorf("persist session bundles: %w", err)
	}
	if err := s.writeBundles(ctx, sess.SessionRoot, sess.Bundles); err != nil {
		return nil, fmt.Errorf("apply claude bundles: %w", err)
	}
	return sess, nil
}

func (s *SessionService) DefaultBundlesFor(ctx context.Context, workspaceIDs []uint) ([]string, error) {
	wss := make([]workspace.Workspace, 0, len(workspaceIDs))
	for _, id := range workspaceIDs {
		ws, err := s.workspaceService.GetWorkspace(ctx, id)
		if err != nil {
			return nil, err
		}
		wss = append(wss, *ws)
	}
	return defaultBundles(ctx, s.sources, wss)
}

func (s *SessionService) writeBundles(ctx context.Context, root string, names []string) error {
	if root == "" {
		return nil
	}
	enabled, servers, unknown := s.bundleCatalog.Resolve(names)
	if len(unknown) > 0 {
		slog.WarnContext(ctx, "session references unknown bundles", "session_root", root, "bundles", unknown)
	}
	selected := make(map[string]json.RawMessage, len(servers))
	var missing []string
	for _, name := range servers {
		def, ok := s.mcpServers[name]
		if !ok {
			missing = append(missing, name)
			continue
		}
		selected[name] = def
	}
	if len(missing) > 0 {
		slog.WarnContext(ctx, "bundle references unknown MCP servers", "session_root", root, "servers", missing)
	}
	managedPlugins, managedServers := s.bundleCatalog.Managed()
	return claudesettings.ApplyBundles(root, claudesettings.BundleSettings{
		ManagedPlugins: managedPlugins,
		Plugins:        enabled,
		ManagedServers: managedServers,
		Servers:        selected,
	})
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

func (s *SessionService) runSetup(sessionID uint, muxName string, specs []WorkspaceBranchSpec) {
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
	slog.InfoContext(ctx, "session setup: start", "mux", muxName, "timeout", s.setupTimeout)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "session setup: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
	}()

	markBroken := func(stage string, err error) { s.markSetupBroken(ctx, sess, stage, err) }

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
		orderedWS = append(orderedWS, newStepWorkspace(ws))
		wsByID[ws.ID] = ws
		swtByID[ws.ID] = swt
	}

	configs, err := s.loadWorkspaceConfigs(ctx, orderedWS)
	if err != nil {
		markBroken("load workspace config", err)
		return
	}
	defs := buildSetupSteps(orderedWS, configs)
	executor := s.newSetupExecution(sess, muxName, defs, orderedWS, specByWS, wsByID, swtByID)
	s.completeSetup(ctx, sess, executor)
}

func (s *SessionService) newSetupExecution(sess *Session, muxName string, defs []stepDef, orderedWS []stepWorkspace, specByWS map[uint]WorkspaceBranchSpec, wsByID map[uint]*workspace.Workspace, swtByID map[uint]*SessionWorktree) *setupExecution {
	steps := s.ensureSetupSteps(sess.ID, defs)
	execSteps := make([]execStep, len(defs))
	for i := range defs {
		execSteps[i] = execStep{def: defs[i], db: steps[i]}
	}
	return &setupExecution{
		svc:       s,
		sess:      sess,
		muxName:   muxName,
		execSteps: execSteps,
		orderedWS: orderedWS,
		specByWS:  specByWS,
		wsByID:    wsByID,
		swtByID:   swtByID,
		updater:   &stepUpdater{store: s.setupStepStore},
		results:   make(map[uint]*worktreeSetupResult, len(orderedWS)),
	}
}

func (s *SessionService) completeSetup(ctx context.Context, sess *Session, executor *setupExecution) {
	executor.run(ctx)

	if executor.broken {
		s.markSetupBroken(ctx, sess, executor.brokenStage, executor.brokenErr)
		return
	}
	if ctx.Err() != nil {
		s.markSetupBroken(ctx, sess, "setup", ctx.Err())
		return
	}

	sess.Status = StatusActive
	if len(executor.warnings) == 0 {
		sess.StatusError = ""
	} else {
		sess.StatusError = strings.Join(executor.warnings, "; ")
	}
	if err := s.store.Update(sess); err != nil {
		s.markSetupBroken(ctx, sess, "persist active status", err)
	}
}

func (s *SessionService) markSetupBroken(ctx context.Context, sess *Session, stage string, err error) {
	msg := fmt.Sprintf("%s failed: %v", stage, err)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		msg = fmt.Sprintf("%s timed out after %s", stage, s.setupTimeout)
	}
	if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
		slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
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

func (s *SessionService) setupMux(ctx context.Context, sess *Session, muxName string, sessionRoot string) error {
	if sess.MuxSessionID != nil {
		record, err := s.muxService.GetSession(*sess.MuxSessionID)
		if err != nil {
			return fmt.Errorf("failed to load mux record: %v", err)
		}
		isFirstSpawn := record.Status == mux.StatusPending

		if s.muxService.HasSession(muxName) {
			slog.InfoContext(ctx, "setup mux: reusing existing mux process", "mux", muxName)
			if _, err := tracedOp(ctx, "mux spawn-for-record", func() (*mux.Session, error) {
				return s.muxService.SpawnForRecord(record.ID)
			}, "mux", muxName, "mux_id", record.ID); err != nil {
				return fmt.Errorf("failed to mark mux active: %v", err)
			}
		} else {
			if _, err := tracedOp(ctx, "mux spawn-for-record", func() (*mux.Session, error) {
				return s.muxService.SpawnForRecord(record.ID)
			}, "mux", muxName, "mux_id", record.ID); err != nil {
				return fmt.Errorf("failed to spawn mux session: %v", err)
			}
		}

		if isFirstSpawn {
			s.dispatchOnCreateActions(ctx, sess, muxName, record.StartDir)
		}
		return nil
	}

	if s.muxService.HasSession(muxName) {
		if ts, err := s.muxService.GetSessionByName(muxName); err == nil {
			sess.MuxSessionID = &ts.ID
		}
		slog.InfoContext(ctx, "setup mux: reusing existing session", "mux", muxName)
		return nil
	}

	startDir := sessionRoot
	if startDir == "" {
		startDir = sess.SessionRoot
	}
	env := map[string]string{EnvSessionID: fmt.Sprintf("%d", sess.ID)}

	ts, err := tracedOp(ctx, "mux new-session", func() (*mux.Session, error) {
		return s.muxService.CreateSession(muxName, startDir, env)
	}, "mux", muxName, "start_dir", startDir)
	if err != nil {
		if !s.muxService.HasSession(muxName) {
			return fmt.Errorf("failed to create mux session: %v", err)
		}
		ts, err = s.muxService.GetOrTrackSession(muxName, startDir, env)
		if err != nil {
			return fmt.Errorf("failed to create mux session: %v", err)
		}
	} else {
		s.dispatchOnCreateActions(ctx, sess, muxName, startDir)
	}
	sess.MuxSessionID = &ts.ID
	return nil
}

func (s *SessionService) dispatchOnCreateActions(ctx context.Context, sess *Session, muxName, startDir string) {
	actions, err := s.sessionActionStore.ListBySessionIDAndTrigger(sess.ID, TriggerOnCreate)
	if err != nil {
		slog.WarnContext(ctx, "failed to load session actions", "session", sess.ID, "error", err)
		return
	}
	if len(actions) == 0 {
		return
	}
	slog.InfoContext(ctx, "setup mux: dispatching on-create actions", "mux", muxName, "count", len(actions))
	go executeSessionActions(actions, s.muxService, s.sessionActionStore, muxName, startDir)
}

func (s *SessionService) runWorkspaceScript(ctx context.Context, ws workspace.Workspace, workDir string, env []string) error {
	relPath := filepath.Join(".utena", worktreeSetupName)
	name := relPath
	localPath := ""
	if resolver, ok := s.sources.(workspace.LocalPathResolver); ok {
		localPath = resolver.SourcePath(ws, relPath)
		name = localPath
	}
	return s.runSourceScript(ctx, name, localPath, workDir, env, func() ([]byte, fs.FileMode, error) {
		return s.sources.ReadSourceFile(ctx, ws, relPath)
	})
}

func (s *SessionService) runGlobalScript(ctx context.Context, workDir string, env []string) error {
	name := "global " + worktreeSetupName
	localPath := ""
	if resolver, ok := s.sources.(workspace.LocalPathResolver); ok {
		localPath = resolver.GlobalSetupScriptPath()
		name = localPath
	}
	return s.runSourceScript(ctx, name, localPath, workDir, env, func() ([]byte, fs.FileMode, error) {
		return s.sources.ReadGlobalSetupScript(ctx)
	})
}

func (s *SessionService) runSourceScript(ctx context.Context, name, localPath, workDir string, env []string, read func() ([]byte, fs.FileMode, error)) error {
	data, mode, err := read()
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return &sourceReadError{err: fmt.Errorf("read script %s: %w", name, err)}
	}
	if mode&0o111 == 0 {
		return fmt.Errorf("script %s exists but is not executable", name)
	}

	path := localPath
	if path == "" {
		staged, err := stageScript(data, mode)
		if err != nil {
			return fmt.Errorf("stage script %s: %w", name, err)
		}
		defer func() { _ = os.Remove(staged) }()
		path = staged
	}
	return runScriptAt(ctx, name, path, workDir, env)
}

func stageScript(data []byte, mode fs.FileMode) (string, error) {
	f, err := os.CreateTemp("", "utena-"+worktreeSetupName+"-*")
	if err != nil {
		return "", err
	}
	path := f.Name()
	_, writeErr := f.Write(data)
	chmodErr := f.Chmod(mode.Perm() | 0o700)
	closeErr := f.Close()
	if err := errors.Join(writeErr, chmodErr, closeErr); err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

func runScriptAt(ctx context.Context, name, path, workDir string, env []string) error {
	cmd := exec.CommandContext(ctx, path)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), env...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("script %s failed: %s: %w", name, strings.TrimSpace(string(output)), err)
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
	if sess.IsCoder() {
		return nil, ErrCoderUnsupported
	}

	if len(sess.Worktrees) == 0 {
		return nil, common.NewInvalidRequest("session has no worktrees; cannot repair — please recreate")
	}

	sess.Status = StatusCreating
	sess.StatusError = ""
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("failed to mark session for repair: %w", err)
	}

	if sess.MuxSession == nil || sess.MuxSession.Name == "" {
		return nil, common.NewInvalidRequest("session has no mux record name; please recreate")
	}
	muxName := sess.MuxSession.Name

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
	go s.runSetup(sess.ID, muxName, specs)
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
	if session.IsCoder() {
		return s.activateCoderSession(ctx, session)
	}

	s.ensureSessionWorktrees(ctx, session)
	s.SyncSessionBranches(ctx, session)
	s.applyClaudeBundles(ctx, session)

	muxName := SanitizeMuxName(session.Name)
	if session.MuxSessionID != nil && session.MuxSession != nil && session.MuxSession.Name != "" {
		muxName = session.MuxSession.Name
	}
	if muxName == "" {
		return nil, fmt.Errorf("session has no mux record; please repair")
	}

	if !s.muxService.HasSession(muxName) {
		startDir := session.SessionRoot
		env := map[string]string{"UTENA_SESSION_ID": fmt.Sprintf("%d", session.ID)}
		ts, createErr := s.muxService.CreateSession(muxName, startDir, env)
		if createErr != nil {
			if !s.muxService.HasSession(muxName) {
				return nil, fmt.Errorf("failed to revive mux session: %w", createErr)
			}
			ts, createErr = s.muxService.GetOrTrackSession(muxName, startDir, env)
			if createErr != nil {
				return nil, fmt.Errorf("failed to revive mux session: %w", createErr)
			}
		} else {
			if actions, err := s.sessionActionStore.ListBySessionIDAndTrigger(session.ID, TriggerOnCreate); err != nil {
				slog.Warn("failed to load session actions", "session", session.ID, "error", err)
			} else if len(actions) > 0 {
				go executeSessionActions(actions, s.muxService, s.sessionActionStore, muxName, startDir)
			}
		}
		session.MuxSessionID = &ts.ID
		session.MuxSession = ts
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
	s.publishSessionEvent(ctx, eventbus.SessionAttached, session.ID)

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
		Data: eventbus.SessionActivatedEvent{SessionName: muxName},
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

	if session.IsCoder() {
		return s.deleteCoderSession(ctx, session)
	}

	if deleteBranch {
		s.SyncSessionBranches(ctx, session)
		if refreshed, err := s.store.GetByID(id); err == nil {
			session = refreshed
		}
	}

	s.cleanupSessionResources(ctx, session, deleteBranch)

	if err := s.store.HardDelete(id); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, eventbus.SessionDeleted, id)

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

// cleanupSessionResources tears down every on-disk and mux resource a session
// owns, continuing past individual failures so a single stuck resource doesn't
// strand the rest of the cleanup.
func (s *SessionService) cleanupSessionResources(ctx context.Context, session *Session, deleteBranch bool) {
	if session.MuxSessionID != nil {
		if err := s.muxService.KillSession(*session.MuxSessionID); err != nil {
			slog.Warn("failed to kill mux session", "error", err)
		}
		session.MuxSessionID = nil
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

	if deleteBranch {
		s.cleanupPastBranches(ctx, session, wsCache)
	}

	s.removeSessionRoot(session)
}

func (s *SessionService) cleanupPastBranches(ctx context.Context, session *Session, wsCache map[uint]*workspace.Workspace) {
	defaultMatchers := make(map[uint]func(string) bool)
	for _, branch := range session.PastBranches() {
		ws, ok := wsCache[branch.RepoID]
		if !ok {
			found, err := s.workspaceService.GetWorkspaceByRepoID(ctx, branch.RepoID)
			if err != nil {
				slog.Warn("cleanup: failed to resolve workspace for past branch", "branch", branch.Name, "error", err)
				continue
			}
			ws = found
			wsCache[branch.RepoID] = ws
		}
		isDefault, ok := defaultMatchers[branch.RepoID]
		if !ok {
			isDefault = s.defaultBranchMatcher(ctx, ws.Path)
			defaultMatchers[branch.RepoID] = isDefault
		}
		if isDefault(branch.Name) {
			continue
		}
		exists, err := s.gitService.HasBranch(ctx, ws.Path, branch.Name)
		if err != nil || !exists {
			continue
		}
		if !s.pastBranchDeletable(ctx, &branch, ws.Path) {
			slog.Warn("keeping past session branch with unpushed work", "session", session.ID, "branch", branch.Name)
			continue
		}
		if err := s.gitService.CleanupBranch(ctx, &branch, ws.Path, true); err != nil {
			slog.Warn("failed to delete past session branch", "session", session.ID, "branch", branch.Name, "error", err)
		}
	}
}

func (s *SessionService) pastBranchDeletable(ctx context.Context, branch *git.Branch, repoPath string) bool {
	head := ""
	for _, pr := range s.gitService.GetPRsForBranch(branch.ID) {
		if pr.State != git.PRStateMerged || pr.HeadSHA == "" {
			continue
		}
		if head == "" {
			resolved, err := s.gitService.BranchHead(ctx, repoPath, branch.Name)
			if err != nil {
				break
			}
			head = resolved
		}
		if head == pr.HeadSHA {
			return true
		}
	}
	unpushed, err := s.gitService.HasUnpushedCommits(ctx, repoPath, branch.Name)
	return err == nil && !unpushed
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

func (s *SessionService) handleMuxSessionCreated(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.MuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.findSessionForHook(data)
	if err != nil {
		slog.Debug("session not found for session-created hook", "mux_name", data.SessionName, "error", err)
		return nil
	}

	if _, err := s.RefreshSession(ctx, sess.ID); err != nil {
		slog.Warn("failed to refresh session after mux create", "session", sess.ID, "error", err)
	}
	return nil
}

func (s *SessionService) handleMuxSessionClosed(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.MuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.findSessionForHook(data)
	if err != nil {
		slog.Debug("session not found for session-closed hook", "mux_name", data.SessionName, "error", err)
		return nil
	}

	sess.IsAttached = false
	sess.Status = StatusBroken
	return s.store.Update(sess)
}

func (s *SessionService) handleMuxClientSessionChanged(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.MuxHookEvent)
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

	sess, err := s.findSessionForHook(data)
	if err != nil {
		slog.Debug("session not found for client-session-changed hook", "mux_name", data.SessionName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	if err := s.store.Update(sess); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, eventbus.SessionAttached, sess.ID)
	return nil
}

func (s *SessionService) handleMuxClientAttached(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.MuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.findSessionForHook(data)
	if err != nil {
		slog.Debug("session not found for client-attached hook", "mux_name", data.SessionName, "error", err)
		return nil
	}

	sess.IsAttached = true
	sess.LastUsedAt = time.Now()
	if err := s.store.Update(sess); err != nil {
		return err
	}
	s.publishSessionEvent(ctx, eventbus.SessionAttached, sess.ID)
	return nil
}

func (s *SessionService) publishSessionEvent(ctx context.Context, eventType string, sessionID uint) {
	if err := s.eventBus.Publish(ctx, eventbus.Event{
		Type: eventType,
		Data: eventbus.SessionIDEvent{SessionID: sessionID},
	}); err != nil {
		slog.Warn("failed to publish session event", "type", eventType, "session", sessionID, "error", err)
	}
}

func (s *SessionService) handleMuxClientDetached(ctx context.Context, event eventbus.Event) error {
	data, ok := event.Data.(eventbus.MuxHookEvent)
	if !ok {
		return fmt.Errorf("unexpected event data type: %T", event.Data)
	}

	sess, err := s.findSessionForHook(data)
	if err != nil {
		slog.Debug("session not found for client-detached hook", "mux_name", data.SessionName, "error", err)
		return nil
	}

	sess.IsAttached = false
	return s.store.Update(sess)
}

func (s *SessionService) reconcileMuxState(ctx context.Context, sessions []Session) {
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
	if sess.IsCoder() {
		return s.archiveCoderSession(ctx, sess)
	}

	if sess.MuxSessionID != nil {
		if err := s.muxService.KillSession(*sess.MuxSessionID); err != nil {
			slog.Warn("failed to kill mux session during archive", "session", sess.ID, "error", err)
		}
		sess.MuxSessionID = nil
	}

	sess.Status = StatusArchived
	sess.IsAttached = false
	if err := s.store.Update(sess); err != nil {
		return nil, err
	}
	s.publishSessionEvent(ctx, eventbus.SessionArchived, sess.ID)
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
		for _, pr := range s.gitService.GetPRsForBranches(sess.BranchIDs()) {
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
	return s.reconcileLoadedSession(ctx, sess)
}

func (s *SessionService) reconcileLoadedSession(ctx context.Context, sess *Session) (*Session, error) {
	if sess.IsCoder() {
		return sess, nil
	}
	if sess.Status == StatusCreating || sess.Status == StatusDeleted || sess.Status == StatusArchived || sess.Status == StatusPending {
		return sess, nil
	}

	muxAlive := false
	if sess.MuxSessionID != nil {
		ts, tsErr := s.muxService.GetSession(*sess.MuxSessionID)
		if tsErr == nil {
			muxAlive = ts.Status == mux.StatusActive
		}
	}

	gitHealthy := s.isSessionGitHealthy(ctx, sess)

	prevStatus := sess.Status
	prevError := sess.StatusError
	canRecoverFromBroken := sess.Status == StatusBroken && sess.StatusError == gitUnhealthyError

	if !gitHealthy {
		sess.Status = StatusBroken
		sess.StatusError = gitUnhealthyError
	} else if !muxAlive {
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

	sess, _ := s.store.GetByBranchID(*pr.HeadBranchID)

	if newlyAssigned && sess == nil {
		s.maybeCreatePendingSession(ctx, data)
		sess, _ = s.store.GetByBranchID(*pr.HeadBranchID)
	}
	if sess == nil {
		return nil
	}

	s.notifyPRUpdated(ctx, sess, pr, data.Previous)

	return nil
}

func (s *SessionService) notifyPRUpdated(ctx context.Context, sess *Session, pr *git.PullRequest, previous *git.PullRequest) {
	data := git.NewPRNotification(pr, previous, sess.branchName(*pr.HeadBranchID))
	s.publishNotification(ctx, sess.ID, git.NotificationPullRequest, data)
}

func (s *SessionService) SessionSnapshot(ctx context.Context, sessionID uint) []eventbus.SessionNotificationEvent {
	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		return nil
	}

	return openPRNotifications(sessionID, s.gitService.GetPRsForBranches(sess.BranchIDs()), sess.branchName)
}

func openPRNotifications(sessionID uint, prs []git.PullRequest, branchName func(uint) string) []eventbus.SessionNotificationEvent {
	var out []eventbus.SessionNotificationEvent
	for i := range prs {
		pr := &prs[i]
		if !pr.State.IsOpen() || pr.HeadBranchID == nil {
			continue
		}
		out = append(out, eventbus.SessionNotificationEvent{
			SessionID: sessionID,
			Type:      git.NotificationPullRequest,
			Data:      git.NewPRNotification(pr, nil, branchName(*pr.HeadBranchID)),
		})
	}
	return out
}

// SyncPRActivity polls reviews, review comments and CI checks for the PRs of
// live sessions and pushes whatever is new to that session's monitor. Only
// session PRs are polled — a PR with no session has nowhere to deliver.
func (s *SessionService) SyncPRActivity(ctx context.Context) error {
	sessions, err := s.store.List()
	if err != nil {
		return err
	}

	// several sessions can sit on one branch, and detecting activity consumes
	// the PR's watermark, so walk each branch once and fan the result out
	order := make([]uint, 0, len(sessions))
	watchers := make(map[uint][]uint)
	for i := range sessions {
		sess := &sessions[i]
		if !sess.watchesPRActivity() {
			continue
		}
		for _, branchID := range sess.BranchIDs() {
			if _, seen := watchers[branchID]; !seen {
				order = append(order, branchID)
			}
			watchers[branchID] = append(watchers[branchID], sess.ID)
		}
	}

	byBranch := groupPRsByBranch(s.gitService.GetPRsForBranches(order))
	for _, branchID := range order {
		for _, pr := range byBranch[branchID] {
			if !pr.State.IsOpen() {
				continue
			}
			activity, err := s.gitService.SyncPRActivity(ctx, &pr)
			if errors.Is(err, git.ErrNoGitHubClient) {
				return err
			}
			if err != nil {
				slog.Warn("failed to sync PR activity", "pr", pr.Number, "error", err)
				continue
			}
			for _, item := range activity {
				for _, sessionID := range watchers[branchID] {
					s.publishNotification(ctx, sessionID, item.Type, item.Data)
				}
			}
		}
	}
	return nil
}

func (s *SessionService) publishNotification(ctx context.Context, sessionID uint, notificationType string, data any) {
	event := eventbus.Event{
		Type: eventbus.SessionNotification,
		Data: eventbus.SessionNotificationEvent{
			SessionID: sessionID,
			Type:      notificationType,
			Data:      data,
		},
	}
	if err := s.eventBus.Publish(ctx, event); err != nil {
		slog.Warn("failed to publish session notification", "session", sessionID, "type", notificationType, "error", err)
	}
}

func (s *SessionService) maybeCreatePendingSession(ctx context.Context, data git.PRUpdatedEvent) {
	pr := data.PullRequest

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

type AddBlockerInput struct {
	Kind BlockerKind
	Note string
	URL  string
}

func (s *SessionService) requireLiveSession(id uint) error {
	status, err := s.store.GetStatus(id)
	if err != nil {
		return err
	}
	if status == StatusDeleted {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SessionService) ownedBlocker(sessionID, blockerID uint) (*SessionBlocker, error) {
	if err := s.requireLiveSession(sessionID); err != nil {
		return nil, err
	}
	b, err := s.blockerStore.GetByID(blockerID)
	if err != nil {
		return nil, err
	}
	if b.SessionID != sessionID {
		return nil, ErrBlockerNotFound
	}
	return b, nil
}

func (s *SessionService) AddBlocker(_ context.Context, sessionID uint, in AddBlockerInput) (*Session, error) {
	if err := s.requireLiveSession(sessionID); err != nil {
		return nil, err
	}
	b := &SessionBlocker{SessionID: sessionID, Kind: in.Kind, Note: in.Note, URL: in.URL}
	if err := s.blockerStore.Create(b); err != nil {
		return nil, fmt.Errorf("failed to add blocker: %w", err)
	}
	return s.store.GetByID(sessionID)
}

func (s *SessionService) ResolveBlocker(_ context.Context, sessionID, blockerID uint) (*Session, error) {
	b, err := s.ownedBlocker(sessionID, blockerID)
	if err != nil {
		return nil, err
	}
	if b.IsOpen() {
		now := time.Now()
		b.ResolvedAt = &now
		if err := s.blockerStore.Update(b); err != nil {
			return nil, fmt.Errorf("failed to resolve blocker: %w", err)
		}
	}
	return s.store.GetByID(sessionID)
}

func (s *SessionService) DeleteBlocker(_ context.Context, sessionID, blockerID uint) error {
	if _, err := s.ownedBlocker(sessionID, blockerID); err != nil {
		return err
	}
	return s.blockerStore.Delete(blockerID)
}
