package session

import (
	"log/slog"

	"github.com/eleonorayaya/utena/internal/claudedata"
)

func (s *SessionService) SetClaudeDataDir(dir string) {
	s.claudeDataDir = dir
}

func (s *SessionService) removeClaudeArtifacts(session *Session) {
	if s.claudeDataDir == "" || !s.sessionRootWithinRoot(session.SessionRoot) {
		return
	}
	shared, err := s.rootSharedWithLiveSession(session)
	if err != nil {
		slog.Warn("failed to list sessions for claude data cleanup", "session", session.ID, "error", err)
		return
	}
	if shared {
		return
	}
	projects, err := claudedata.Projects(s.claudeDataDir)
	if err != nil {
		slog.Warn("failed to list claude projects", "dir", s.claudeDataDir, "error", err)
		return
	}
	for _, p := range projects {
		if !claudedata.Within(p.CWD, session.SessionRoot) {
			continue
		}
		if err := claudedata.Remove(s.claudeDataDir, p); err != nil {
			slog.Warn("failed to remove claude project data", "session", session.ID, "dir", p.Dir, "error", err)
		}
	}
}

func (s *SessionService) rootSharedWithLiveSession(session *Session) (bool, error) {
	sessions, err := s.store.List()
	if err != nil {
		return false, err
	}
	for _, other := range sessions {
		if other.ID == session.ID || other.Status == StatusDeleted {
			continue
		}
		if claudedata.Within(other.SessionRoot, session.SessionRoot) || claudedata.Within(session.SessionRoot, other.SessionRoot) {
			return true, nil
		}
	}
	return false, nil
}
