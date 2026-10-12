package session

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/eleonorayaya/utena/internal/git"
)

func canonicalPath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return resolved
	}
	return filepath.Clean(p)
}

func (s *SessionService) RecordWorktreeBranch(ctx context.Context, path string, branchName string) error {
	if branchName == "" || path == "" {
		return nil
	}
	sess, swt, err := s.findSessionWorktreeByPath(path)
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}
	return s.recordBranch(ctx, sess, swt, branchName)
}

func (s *SessionService) findSessionWorktreeByPath(path string) (*Session, *SessionWorktree, error) {
	target := canonicalPath(path)
	sessions, err := s.store.List()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list sessions: %w", err)
	}
	for i := range sessions {
		sess := &sessions[i]
		if sess.Status == StatusDeleted {
			continue
		}
		for j := range sess.Worktrees {
			wt := sess.Worktrees[j].Worktree
			if wt != nil && wt.Path != "" && canonicalPath(wt.Path) == target {
				return sess, &sess.Worktrees[j], nil
			}
		}
	}
	return nil, nil, nil
}

func (s *SessionService) recordBranch(ctx context.Context, sess *Session, swt *SessionWorktree, branchName string) error {
	wt := swt.Worktree
	if wt == nil || branchName == "" {
		return nil
	}
	if wt.Branch != nil && wt.Branch.Name == branchName {
		return nil
	}
	ws := swt.Workspace
	if ws == nil {
		return fmt.Errorf("worktree %d has no workspace", wt.ID)
	}
	if branchName == s.gitService.DefaultBranch(ctx, ws.Path) {
		return nil
	}

	branch, err := s.gitService.FindOrCreateBranch(ctx, branchName, wt.RepoID)
	if err != nil {
		return fmt.Errorf("find-or-create branch %q: %w", branchName, err)
	}

	now := time.Now()
	for _, id := range []uint{wt.BranchID, branch.ID} {
		if id == 0 {
			continue
		}
		if err := s.sessionBranchStore.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: wt.ID, BranchID: id, FirstSeenAt: now}); err != nil {
			return fmt.Errorf("record session branch: %w", err)
		}
	}

	if err := s.gitService.RepointWorktree(wt, branch); err != nil {
		slog.WarnContext(ctx, "failed to repoint worktree to new branch", "session", sess.ID, "worktree", wt.ID, "branch", branchName, "error", err)
	}
	slog.InfoContext(ctx, "session branch attached", "session", sess.ID, "worktree", wt.ID, "branch", branchName)

	s.replayOpenPRs(ctx, sess.ID, branch)
	return nil
}

func (s *SessionService) replayOpenPRs(ctx context.Context, sessionID uint, branch *git.Branch) {
	for _, pr := range s.gitService.GetPRsForBranch(branch.ID) {
		if !pr.State.IsOpen() {
			continue
		}
		s.publishNotification(ctx, sessionID, git.NotificationPullRequest, git.NewPRNotification(&pr, nil, branch.Name))
	}
}

func (s *SessionService) SyncSessionBranches(ctx context.Context, sess *Session) {
	for i := range sess.Worktrees {
		swt := &sess.Worktrees[i]
		wt := swt.Worktree
		if wt == nil || wt.Path == "" || wt.Status != git.WorktreeStatusPresent {
			continue
		}
		name, err := s.gitService.CurrentBranch(ctx, wt.Path)
		if err != nil {
			slog.DebugContext(ctx, "branch sync: current branch lookup failed", "session", sess.ID, "worktree", wt.ID, "error", err)
			continue
		}
		if err := s.recordBranch(ctx, sess, swt, name); err != nil {
			slog.WarnContext(ctx, "branch sync failed", "session", sess.ID, "worktree", wt.ID, "error", err)
		}
	}
}

func (s *SessionService) SyncAllSessionBranches(ctx context.Context) error {
	sessions, err := s.store.List()
	if err != nil {
		return fmt.Errorf("failed to list sessions: %w", err)
	}
	for i := range sessions {
		if sessions[i].watchesPRActivity() {
			s.SyncSessionBranches(ctx, &sessions[i])
		}
	}
	return nil
}
