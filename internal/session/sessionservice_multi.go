package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eleonorayaya/utena/internal/claudesettings"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
	slogctx "github.com/veqryn/slog-context"
)

type CreateMultiSessionInput struct {
	Name         string
	WorkspaceIDs []uint
	Branch       string
	BaseBranch   string
	TodoID       *uint
	Actions      []*SessionAction
}

type multiSlot struct {
	workspace  *workspace.Workspace
	subdirName string
	destPath   string
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

func (s *SessionService) CreateMultiSession(ctx context.Context, input CreateMultiSessionInput) (*Session, error) {
	if input.Name == "" {
		return nil, common.NewInvalidRequest("name is required for multi-workspace session")
	}
	if err := ValidateSessionName(input.Name); err != nil {
		return nil, common.NewInvalidRequest(err.Error())
	}
	if len(input.WorkspaceIDs) < 2 {
		return nil, common.NewInvalidRequest("multi-workspace session requires at least 2 workspaces")
	}
	if input.Branch == "" {
		return nil, common.NewInvalidRequest("branch is required for multi-workspace session")
	}
	if s.sessionsRoot == "" {
		return nil, common.NewInvalidRequest("sessions root is not configured")
	}

	seen := make(map[uint]struct{}, len(input.WorkspaceIDs))
	for _, id := range input.WorkspaceIDs {
		if _, dup := seen[id]; dup {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %d listed more than once", id))
		}
		seen[id] = struct{}{}
	}
	slots := make([]multiSlot, 0, len(input.WorkspaceIDs))
	for _, id := range input.WorkspaceIDs {
		ws, err := s.workspaceService.GetWorkspace(ctx, id)
		if err != nil {
			return nil, err
		}
		if !ws.IsGitRepo {
			return nil, common.NewInvalidRequest(fmt.Sprintf("workspace %q is not a git repository; multi-workspace sessions require all workspaces to be git", ws.Name))
		}
		slots = append(slots, multiSlot{workspace: ws})
	}

	sessionRoot := filepath.Join(s.sessionsRoot, SanitizeSessionName(input.Name))
	if _, err := os.Stat(sessionRoot); err == nil {
		return nil, common.NewInvalidRequest(fmt.Sprintf("session root %q already exists on disk", sessionRoot))
	}

	usedSubdirs := make(map[string]struct{}, len(slots))
	for i := range slots {
		base := SanitizeSessionName(slots[i].workspace.Name)
		if base == "" {
			base = fmt.Sprintf("workspace-%d", slots[i].workspace.ID)
		}
		name := base
		suffix := 2
		for {
			if _, taken := usedSubdirs[name]; !taken {
				break
			}
			name = fmt.Sprintf("%s-%d", base, suffix)
			suffix++
		}
		usedSubdirs[name] = struct{}{}
		slots[i].subdirName = name
		slots[i].destPath = filepath.Join(sessionRoot, name)
	}

	tmuxName := SanitizeTmuxName(input.Name)

	for _, w := range slots {
		_, err := s.store.GetByWorkspaceAndName(w.workspace.ID, input.Name, StatusDeleted, StatusArchived)
		if err == nil {
			return nil, fmt.Errorf("session %q already exists in workspace %q: %w", input.Name, w.workspace.Name, ErrSessionAlreadyExists)
		}
		if !errors.Is(err, ErrSessionNotFound) {
			return nil, fmt.Errorf("failed to check for duplicate session: %w", err)
		}
	}

	if err := os.MkdirAll(sessionRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create session root %q: %w", sessionRoot, err)
	}

	sess := &Session{
		Name:        input.Name,
		TodoID:      input.TodoID,
		Status:      StatusCreating,
		LastUsedAt:  time.Now(),
		SessionRoot: sessionRoot,
	}
	if err := s.store.Add(sess); err != nil {
		_ = os.RemoveAll(sessionRoot)
		return nil, err
	}

	for i, w := range slots {
		sw := &SessionWorkspace{
			SessionID:   sess.ID,
			WorkspaceID: w.workspace.ID,
			Position:    i,
		}
		if err := s.sessionWorkspaceStore.Add(sw); err != nil {
			if delErr := s.store.Delete(sess.ID); delErr != nil {
				slog.ErrorContext(ctx, "failed to delete session after junction add failure", "session", sess.ID, "error", delErr)
			}
			_ = os.RemoveAll(sessionRoot)
			return nil, fmt.Errorf("add session-workspace junction for workspace %d: %w", w.workspace.ID, err)
		}
	}

	for _, action := range input.Actions {
		action.SessionID = sess.ID
		if err := s.sessionActionStore.Add(action); err != nil {
			slog.ErrorContext(ctx, "failed to persist session action", "session", sess.ID, "trigger", action.Trigger, "error", err)
		}
	}

	tmuxEnv := map[string]string{envSessionID: fmt.Sprintf("%d", sess.ID)}
	tmuxRecord, err := s.tmuxService.RegisterPending(tmuxName, sessionRoot, tmuxEnv)
	if err != nil {
		slog.WarnContext(ctx, "failed to register pending tmux record", "session", sess.ID, "tmux", tmuxName, "error", err)
	} else {
		sess.TmuxSessionID = &tmuxRecord.ID
		if err := s.store.Update(sess); err != nil {
			slog.WarnContext(ctx, "failed to persist tmux session id", "session", sess.ID, "error", err)
		}
	}

	for _, w := range slots {
		if err := s.workspaceService.Touch(ctx, w.workspace.ID); err != nil {
			slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", w.workspace.ID, "error", err)
		}
	}

	go s.runMultiSetup(sess.ID, slots, sessionRoot, tmuxName, input.Branch, input.BaseBranch)

	return sess, nil
}

func (s *SessionService) runMultiSetup(sessionID uint, slots []multiSlot, sessionRoot, tmuxName, branchName, baseBranchName string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runMultiSetup: failed to load session", "id", sessionID, "error", err)
		return
	}

	ctx = slogctx.Append(ctx,
		"session", sess.ID,
		"branch", branchName,
		"base_branch", baseBranchName,
		"session_root", sessionRoot,
		"workspace_count", len(slots),
	)
	slog.InfoContext(ctx, "multi-workspace session setup: start", "tmux", tmuxName, "timeout", s.setupTimeout)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "multi-workspace session setup: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
	}()

	type slotResult struct {
		slot         multiSlot
		branch       *git.Branch
		worktreePath string
	}
	var done []slotResult
	rollback := func() {
		for i := len(done) - 1; i >= 0; i-- {
			r := done[i]
			if r.worktreePath == "" {
				continue
			}
			if err := s.gitService.RemoveWorktree(ctx, r.slot.workspace.Path, r.worktreePath); err != nil {
				slog.WarnContext(ctx, "rollback: failed to remove worktree", "workspace", r.slot.workspace.Name, "path", r.worktreePath, "error", err)
			}
		}
		if err := os.RemoveAll(sessionRoot); err != nil {
			slog.WarnContext(ctx, "rollback: failed to remove session root", "path", sessionRoot, "error", err)
		}
	}

	markBroken := func(stage string, w *workspace.Workspace, err error) {
		var msg string
		switch {
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			msg = fmt.Sprintf("%s timed out after %s (workspace %q)", stage, s.setupTimeout, w.Name)
		default:
			msg = fmt.Sprintf("%s failed for workspace %q: %v", stage, w.Name, err)
		}
		rollback()
		if updateErr := s.markSessionBroken(sess, msg); updateErr != nil {
			slog.ErrorContext(ctx, "failed to persist broken status", "stage", stage, "error", updateErr)
		}
	}

	junctionRows, err := s.sessionWorkspaceStore.ListBySessionID(sess.ID)
	if err != nil {
		markBroken("load junction rows", slots[0].workspace, err)
		return
	}
	junctionByWorkspaceID := map[uint]*SessionWorkspace{}
	for i := range junctionRows {
		junctionByWorkspaceID[junctionRows[i].WorkspaceID] = &junctionRows[i]
	}

	creatingNew := baseBranchName != ""
	finalBranchName := branchName
	if creatingNew {
		finalBranchName = s.branchPrefix + sess.Name
	}

	var setupWarnings []string

	for _, slot := range slots {
		ws := slot.workspace
		if err := s.setupBranch(ctx, ws, branchName, baseBranchName); err != nil {
			var w SetupWarning
			if errors.As(err, &w) {
				setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", ws.Name, w.Message))
			} else {
				markBroken("branch setup", ws, err)
				return
			}
		}

		repoID, err := s.resolveRepoID(ctx, ws)
		if err != nil {
			markBroken("repo lookup", ws, err)
			return
		}

		branch, err := tracedOp(ctx, "find-or-create-branch", func() (*git.Branch, error) {
			return s.gitService.FindOrCreateBranch(ctx, finalBranchName, repoID)
		}, "name", finalBranchName, "ws", ws.Name)
		if err != nil {
			markBroken("find-or-create-branch", ws, err)
			return
		}

		var wtPath string
		_, wtPath, err = s.gitService.SetupWorktreeAt(ctx, ws.Path, finalBranchName, baseBranchName, branch.ID, repoID, slot.destPath)
		if err != nil {
			markBroken("worktree setup", ws, err)
			return
		}

		if row := junctionByWorkspaceID[ws.ID]; row != nil {
			row.BranchID = &branch.ID
			row.WorktreePath = wtPath
			if updateErr := s.sessionWorkspaceStore.Update(row); updateErr != nil {
				slog.WarnContext(ctx, "failed to update session workspace junction", "workspace", ws.ID, "error", updateErr)
			}
		}

		done = append(done, slotResult{slot: slot, branch: branch, worktreePath: wtPath})
	}

	for _, r := range done {
		env := []string{
			envSessionRoot + "=" + sessionRoot,
			envSessionName + "=" + sess.Name,
			envBranch + "=" + finalBranchName,
			envWorktreePath + "=" + r.worktreePath,
			envWorkspaceName + "=" + r.slot.workspace.Name,
			envWorkspacePath + "=" + r.slot.workspace.Path,
		}
		wsScript := filepath.Join(r.slot.workspace.Path, ".utena", worktreeSetupName)
		if err := traceOp(ctx, "worktree-setup script", func() error {
			return s.runScript(ctx, wsScript, r.worktreePath, env)
		}, "script", wsScript, "ws", r.slot.workspace.Name); err != nil {
			slog.WarnContext(ctx, "workspace worktree-setup script failed", "ws", r.slot.workspace.Name, "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("%s: %s", r.slot.workspace.Name, err.Error()))
		}
	}

	globalEnv := []string{
		envSessionRoot + "=" + sessionRoot,
		envSessionName + "=" + sess.Name,
		envBranch + "=" + finalBranchName,
	}
	globalScript := filepath.Join(s.configDir, worktreeSetupName)
	if err := traceOp(ctx, "global worktree-setup script", func() error {
		return s.runScript(ctx, globalScript, sessionRoot, globalEnv)
	}, "script", globalScript); err != nil {
		slog.WarnContext(ctx, "global worktree-setup script failed", "error", err)
		setupWarnings = append(setupWarnings, fmt.Sprintf("global: %s", err.Error()))
	}

	gitDirs := make([]string, 0, len(done))
	for _, r := range done {
		gitDirs = append(gitDirs, filepath.Join(r.worktreePath, ".git"))
	}
	if err := claudesettings.EnsureSessionRoot(sessionRoot, gitDirs); err != nil {
		slog.WarnContext(ctx, "ensure session-root claude settings failed", "error", err)
		setupWarnings = append(setupWarnings, fmt.Sprintf("claude-settings: %s", err.Error()))
	}

	if err := s.setupTmux(ctx, sess, tmuxName, sessionRoot); err != nil {
		markBroken("tmux setup", slots[0].workspace, err)
		return
	}

	sess.Status = StatusActive
	sess.StatusError = strings.Join(setupWarnings, "; ")
	if err := s.store.Update(sess); err != nil {
		markBroken("persist active status", slots[0].workspace, err)
		return
	}
}

func (s *SessionService) runMultiRepair(sessionID uint, tmuxName string) {
	ctx, cancel := context.WithTimeout(context.Background(), s.setupTimeout)
	defer cancel()

	sess, err := s.store.GetByID(sessionID)
	if err != nil {
		slog.Error("runMultiRepair: failed to load session", "id", sessionID, "error", err)
		return
	}

	ctx = slogctx.Append(ctx, "session", sess.ID, "session_root", sess.SessionRoot, "workspace_count", len(sess.Workspaces))
	slog.InfoContext(ctx, "multi-workspace session repair: start", "tmux", tmuxName, "timeout", s.setupTimeout)
	setupStart := time.Now()
	defer func() {
		slog.InfoContext(ctx, "multi-workspace session repair: done", "status", sess.Status, "duration_ms", time.Since(setupStart).Milliseconds())
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

	if sess.SessionRoot != "" {
		if err := os.MkdirAll(sess.SessionRoot, 0o755); err != nil {
			markBroken("ensure session root", err)
			return
		}
	}

	var setupWarnings []string
	gitDirs := make([]string, 0, len(sess.Workspaces))
	for i := range sess.Workspaces {
		sw := &sess.Workspaces[i]
		if sw.Workspace == nil || sw.GitBranch == nil || sw.WorktreePath == "" {
			continue
		}
		repoID := uint(0)
		if sw.Workspace.RepoID != nil {
			repoID = *sw.Workspace.RepoID
		}
		if err := s.gitService.PruneWorktrees(ctx, sw.Workspace.Path); err != nil {
			slog.WarnContext(ctx, "prune worktrees before repair failed", "workspace", sw.Workspace.Name, "error", err)
		}
		if _, _, err := s.gitService.SetupWorktreeAt(ctx, sw.Workspace.Path, sw.GitBranch.Name, "", sw.GitBranch.ID, repoID, sw.WorktreePath); err != nil {
			markBroken(fmt.Sprintf("worktree repair for workspace %q", sw.Workspace.Name), err)
			return
		}
		gitDirs = append(gitDirs, filepath.Join(sw.WorktreePath, ".git"))
	}

	if len(gitDirs) > 0 && sess.SessionRoot != "" {
		if err := claudesettings.EnsureSessionRoot(sess.SessionRoot, gitDirs); err != nil {
			slog.WarnContext(ctx, "ensure session-root claude settings failed", "error", err)
			setupWarnings = append(setupWarnings, fmt.Sprintf("claude-settings: %s", err.Error()))
		}
	}

	if err := s.setupTmux(ctx, sess, tmuxName, sess.SessionRoot); err != nil {
		markBroken("tmux setup", err)
		return
	}

	sess.Status = StatusActive
	sess.StatusError = strings.Join(setupWarnings, "; ")
	if err := s.store.Update(sess); err != nil {
		markBroken("persist active status", err)
		return
	}
}
