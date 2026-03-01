package claude

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/spf13/afero"
)

type ClaudeStore struct {
	mu        sync.RWMutex
	sessions  map[string]*ClaudeSession
	fs        afero.Fs
	configDir string
}

func NewClaudeStore(fs afero.Fs, configDir string) *ClaudeStore {
	return &ClaudeStore{
		sessions:  make(map[string]*ClaudeSession),
		fs:        fs,
		configDir: configDir,
	}
}

func (s *ClaudeStore) GetByID(id string) (*ClaudeSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrClaudeSessionNotFound
	}

	copy := *session
	return &copy, nil
}

func (s *ClaudeStore) List() []ClaudeSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]ClaudeSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, *session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdatedAt.After(sessions[j].LastUpdatedAt)
	})

	return sessions
}

func (s *ClaudeStore) ListBySessionID(sessionID string) []ClaudeSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]ClaudeSession, 0)
	for _, session := range s.sessions {
		if session.SessionID == sessionID {
			sessions = append(sessions, *session)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUpdatedAt.After(sessions[j].LastUpdatedAt)
	})

	return sessions
}

func (s *ClaudeStore) Upsert(session *ClaudeSession) error {
	if session == nil {
		return errors.New("claude session cannot be nil")
	}

	if session.ID == "" {
		return errors.New("claude session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	copy := *session
	s.sessions[session.ID] = &copy
	s.save()
	return nil
}

func (s *ClaudeStore) UpdateStatusBySessionID(sessionID string, from, to ClaudeSessionStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed := false
	for _, session := range s.sessions {
		if session.SessionID == sessionID && session.Status == from {
			session.Status = to
			session.LastUpdatedAt = time.Now()
			changed = true
		}
	}

	if changed {
		s.save()
	}
}

func (s *ClaudeStore) Delete(id string) error {
	if id == "" {
		return errors.New("claude session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return ErrClaudeSessionNotFound
	}

	delete(s.sessions, id)
	s.save()
	return nil
}

func (s *ClaudeStore) sessionsPath() string {
	return filepath.Join(s.configDir, "claude_sessions.json")
}

func (s *ClaudeStore) save() {
	s.fs.MkdirAll(s.configDir, 0755)

	data, err := json.Marshal(s.sessions)
	if err != nil {
		return
	}

	afero.WriteFile(s.fs, s.sessionsPath(), data, 0644)
}

func (s *ClaudeStore) OnAppStart(ctx context.Context) error {
	data, err := afero.ReadFile(s.fs, s.sessionsPath())
	if err != nil {
		return nil
	}

	loaded := make(map[string]*ClaudeSession)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = loaded

	return nil
}

func (s *ClaudeStore) OnAppEnd(ctx context.Context) error {
	return nil
}
