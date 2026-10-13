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
	isDefault := s.defaultBranchMatcher(ctx, ws.Path)
	if isDefault(branchName) {
		return nil
	}

	branch, err := s.gitService.FindOrCreateBranch(ctx, branchName, wt.RepoID)
	if err != nil {
		return fmt.Errorf("find-or-create branch %q: %w", branchName, err)
	}

	now := time.Now()
	recorded := []uint{branch.ID}
	if wt.BranchID != 0 && (wt.Branch == nil || !isDefault(wt.Branch.Name)) {
		recorded = []uint{wt.BranchID, branch.ID}
	}
	for _, id := range recorded {
		if err := s.sessionBranchStore.Add(&SessionBranch{SessionID: sess.ID, WorktreeID: wt.ID, BranchID: id, FirstSeenAt: now}); err != nil {
			return fmt.Errorf("record session branch: %w", err)
		}
	}

	if err := s.gitService.RepointWorktree(wt, branch); err != nil {
		slog.WarnContext(ctx, "failed to repoint worktree to new branch", "session", sess.ID, "worktree", wt.ID, "branch", branchName, "error", err)
		return nil
	}
	slog.InfoContext(ctx, "session branch attached", "session", sess.ID, "worktree", wt.ID, "branch", branchName)

	for _, event := range openPRNotifications(sess.ID, s.gitService.GetPRsForBranch(branch.ID), func(uint) string { return branch.Name }) {
		s.publishNotification(ctx, event.SessionID, event.Type, event.Data)
	}
	return nil
}

func (s *SessionService) defaultBranchMatcher(ctx context.Context, repoPath string) func(string) bool {
	def := s.gitService.DefaultBranch(ctx, repoPath)
	return func(name string) bool {
		if name == "" {
			return false
		}
		if def != "" {
			return name == def
		}
		return name == "main" || name == "master"
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
	s.syncWatchedSessionBranches(ctx, sessions)
	return nil
}

func (s *SessionService) syncWatchedSessionBranches(ctx context.Context, sessions []Session) {
	for i := range sessions {
		if sessions[i].watchesPRActivity() {
			s.SyncSessionBranches(ctx, &sessions[i])
		}
	}
}
