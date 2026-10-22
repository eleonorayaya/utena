package session

import (
	"log/slog"
	"time"

	"github.com/eleonorayaya/utena/internal/claudedata"
)

const claudeArtifactQuietPeriod = 10 * time.Minute

func (s *SessionService) SetClaudeDataDir(dir string) {
	s.claudeDataDir = dir
}

func (s *SessionService) removeClaudeArtifacts(session *Session) {
	if s.claudeDataDir == "" || !s.sessionRootWithinRoot(session.SessionRoot) {
		return
	}
	live, err := s.liveSessionRoots(session.ID)
	if err != nil {
		slog.Warn("failed to list sessions for claude data cleanup", "session", session.ID, "error", err)
		return
	}
	s.removeClaudeProjects(func(p claudedata.Project) bool {
		return claudedata.Within(p.CWD, session.SessionRoot)
	}, live)
}

func (s *SessionService) sweepClaudeArtifacts() {
	if s.claudeDataDir == "" || s.sessionsRoot == "" {
		return
	}
	live, err := s.liveSessionRoots(0)
	if err != nil {
		slog.Warn("failed to list sessions for claude data sweep", "error", err)
		return
	}
	cutoff := time.Now().Add(-claudeArtifactQuietPeriod)
	s.removeClaudeProjects(func(p claudedata.Project) bool {
		return s.sessionRootWithinRoot(p.CWD) && p.ModTime.Before(cutoff)
	}, live)
}

func (s *SessionService) liveSessionRoots(exclude uint) ([]string, error) {
	sessions, err := s.store.List()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, sess := range sessions {
		if sess.ID == exclude || sess.Status == StatusDeleted || sess.SessionRoot == "" {
			continue
		}
		roots = append(roots, sess.SessionRoot)
	}
	return roots, nil
}

func (s *SessionService) removeClaudeProjects(match func(claudedata.Project) bool, live []string) {
	projects, err := claudedata.Projects(s.claudeDataDir)
	if err != nil {
		slog.Warn("failed to list claude projects", "dir", s.claudeDataDir, "error", err)
		return
	}
	removed := 0
	for _, p := range projects {
		if !match(p) || withinAny(p.CWD, live) {
			continue
		}
		if err := claudedata.Remove(s.claudeDataDir, p); err != nil {
			slog.Warn("failed to remove claude project data", "dir", p.Dir, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("removed claude data for deleted sessions", "projects", removed)
	}
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if claudedata.Within(path, root) {
			return true
		}
	}
	return false
}
