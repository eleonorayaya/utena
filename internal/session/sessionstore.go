package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync"

	"github.com/spf13/afero"
)

type SessionStore struct {
	mu        sync.RWMutex
	sessions  map[string]*Session
	fs        afero.Fs
	configDir string
}

func NewSessionStore(fs afero.Fs, configDir string) *SessionStore {
	return &SessionStore{
		sessions:  make(map[string]*Session),
		fs:        fs,
		configDir: configDir,
	}
}

func (s *SessionStore) GetByID(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	session, ok := s.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}

	copy := *session
	return &copy, nil
}

func (s *SessionStore) List() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]Session, 0, len(s.sessions))
	for _, session := range s.sessions {
		sessions = append(sessions, *session)
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsedAt.After(sessions[j].LastUsedAt)
	})

	return sessions
}

func (s *SessionStore) ListByWorkspace(workspaceID string) []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]Session, 0)
	for _, session := range s.sessions {
		if session.WorkspaceID == workspaceID {
			sessions = append(sessions, *session)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastUsedAt.After(sessions[j].LastUsedAt)
	})

	return sessions
}

func (s *SessionStore) sessionsPath() string {
	return filepath.Join(s.configDir, "sessions.json")
}

func (s *SessionStore) save() {
	s.fs.MkdirAll(s.configDir, 0755)

	data, err := json.Marshal(s.sessions)
	if err != nil {
		return
	}

	afero.WriteFile(s.fs, s.sessionsPath(), data, 0644)
}

func (s *SessionStore) Add(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if session.ID == "" {
		return errors.New("session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.ID]; exists {
		return fmt.Errorf("session '%s' already exists: %w", session.ID, ErrSessionAlreadyExists)
	}

	copy := *session
	s.sessions[session.ID] = &copy
	s.save()
	return nil
}

func (s *SessionStore) Update(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}

	if session.ID == "" {
		return errors.New("session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[session.ID]; !exists {
		return ErrSessionNotFound
	}

	s.sessions[session.ID] = session
	s.save()
	return nil
}

func (s *SessionStore) Delete(id string) error {
	if id == "" {
		return errors.New("session ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.sessions[id]; !exists {
		return ErrSessionNotFound
	}

	delete(s.sessions, id)
	s.save()
	return nil
}

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	data, err := afero.ReadFile(s.fs, s.sessionsPath())
	if err != nil {
		return nil
	}

	loaded := make(map[string]*Session)
	if err := json.Unmarshal(data, &loaded); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = loaded

	for _, session := range s.sessions {
		if session.Name == "" {
			session.Name = session.ID
		}
	}

	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}
