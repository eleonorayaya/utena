package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/workspace"
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
	workspace *workspace.Workspace
	destPath  string
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
		if err := s.eagerCreateWorktree(ctx, sess.ID, w.workspace, input.Branch, w.destPath, i); err != nil {
			if delErr := s.store.Delete(sess.ID); delErr != nil {
				slog.ErrorContext(ctx, "failed to delete session after worktree creation failure", "session", sess.ID, "error", delErr)
			}
			_ = os.RemoveAll(sessionRoot)
			return nil, fmt.Errorf("eager create worktree for workspace %d: %w", w.workspace.ID, err)
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
		if delErr := s.store.Delete(sess.ID); delErr != nil {
			slog.WarnContext(ctx, "failed to roll back session after tmux registration failure", "session", sess.ID, "error", delErr)
		}
		_ = os.RemoveAll(sessionRoot)
		return nil, fmt.Errorf("register tmux session %q: %w", tmuxName, err)
	}
	sess.TmuxSessionID = &tmuxRecord.ID
	if err := s.store.Update(sess); err != nil {
		return nil, fmt.Errorf("persist tmux session id: %w", err)
	}

	for _, w := range slots {
		if err := s.workspaceService.Touch(ctx, w.workspace.ID); err != nil {
			slog.WarnContext(ctx, "failed to touch workspace last-used timestamp", "workspace", w.workspace.ID, "error", err)
		}
	}

	go s.runSetup(sess.ID, tmuxName, input.Branch, input.BaseBranch)

	return sess, nil
}
