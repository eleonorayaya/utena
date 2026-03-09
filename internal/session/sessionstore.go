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
	if session.Resources != nil {
		resCopy := *session.Resources
		if session.Resources.Branch != nil {
			brCopy := *session.Resources.Branch
			resCopy.Branch = &brCopy
		}
		if session.Resources.Worktree != nil {
			wtCopy := *session.Resources.Worktree
			resCopy.Worktree = &wtCopy
		}
		if session.Resources.Tmux != nil {
			tmCopy := *session.Resources.Tmux
			resCopy.Tmux = &tmCopy
		}
		copy.Resources = &resCopy
	}
	return &copy, nil
}

func (s *SessionStore) GetByTmuxName(tmuxName string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, session := range s.sessions {
		if session.TmuxSessionName == tmuxName {
			copy := *session
			return &copy, nil
		}
	}

	return nil, ErrSessionNotFound
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

type legacySession struct {
	Session
	IsActive      bool     `json:"is_active"`
	IsDead        bool     `json:"is_dead"`
	IsDeleted     bool     `json:"is_deleted"`
	BranchName    string   `json:"branch_name,omitempty"`
	WorkspaceName string   `json:"workspace_name,omitempty"`
	Cleanup       *Cleanup `json:"cleanup,omitempty"`
}

type Cleanup struct {
	TmuxSessionKilled bool `json:"tmux_session_killed"`
	WorktreeRemoved   bool `json:"worktree_removed"`
	BranchDeleted     bool `json:"branch_deleted"`
}

func (s *SessionStore) OnAppStart(ctx context.Context) error {
	data, err := afero.ReadFile(s.fs, s.sessionsPath())
	if err != nil {
		return nil
	}

	legacy := make(map[string]*legacySession)
	if err := json.Unmarshal(data, &legacy); err != nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions = make(map[string]*Session, len(legacy))
	for id, ls := range legacy {
		sess := &ls.Session
		sess.ID = id

		if sess.Name == "" {
			sess.Name = sess.ID
		}
		if sess.TmuxSessionName == "" {
			sess.TmuxSessionName = sess.ID
		}

		if ls.BranchName != "" && sess.Branch == "" {
			sess.Branch = ls.BranchName
		}

		if sess.Status == "" {
			switch {
			case ls.IsDeleted:
				sess.Status = StatusDeleted
			case ls.IsDead:
				sess.Status = StatusBroken
			default:
				sess.Status = StatusReady
			}
		}

		if sess.Resources == nil {
			res := &Resources{}
			if sess.Branch != "" {
				status := ResourceReady
				if sess.Status == StatusBroken {
					status = ResourceReady
				}
				res.Branch = &ResourceState{Status: status}
			}
			if sess.WorktreePath != "" {
				res.Worktree = &ResourceState{Status: ResourceReady}
			}
			tmuxStatus := ResourceReady
			if sess.Status == StatusBroken {
				tmuxStatus = ResourceRemoved
			} else if sess.Status == StatusDeleted {
				tmuxStatus = ResourceRemoved
			}
			res.Tmux = &ResourceState{Status: tmuxStatus}
			sess.Resources = res
		}

		s.sessions[id] = sess
	}

	s.save()
	return nil
}

func (s *SessionStore) OnAppEnd(ctx context.Context) error {
	return nil
}
