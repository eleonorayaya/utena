package session

import (
	"log/slog"

	"github.com/eleonorayaya/utena/internal/claudedata"
	"github.com/eleonorayaya/utena/internal/common"
)

func (s *SessionService) SetClaudeDataDir(dir string) {
	s.claudeDataDir = dir
}

func (s *SessionService) removeClaudeArtifacts(session *Session) {
	if s.claudeDataDir == "" || !s.sessionRootWithinRoot(session.SessionRoot) {
		return
	}
	roots, err := s.store.LiveSessionRoots(session.ID)
	if err != nil {
		slog.Warn("failed to list sessions for claude data cleanup", "session", session.ID, "error", err)
		return
	}
	for _, root := range roots {
		if common.PathWithin(root, session.SessionRoot) || common.PathWithin(session.SessionRoot, root) {
			return
		}
	}
	projects, err := claudedata.ProjectsUnder(s.claudeDataDir, session.SessionRoot)
	if err != nil {
		slog.Warn("failed to list claude projects", "dir", s.claudeDataDir, "error", err)
		return
	}
	for _, p := range projects {
		if err := claudedata.Remove(s.claudeDataDir, p); err != nil {
			slog.Warn("failed to remove claude project data", "session", session.ID, "dir", p.Dir, "error", err)
		}
	}
}
