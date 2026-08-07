package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	statusActive    = "active"
	statusCompleted = "completed"
	statusArchived  = "archived"
)

type Checkout struct {
	Repo        string `json:"repo"`
	Label       string `json:"label"`
	Path        string `json:"path"`
	Branch      string `json:"branch"`
	WorkspaceID string `json:"workspace_id"`
}

type Session struct {
	Name        string     `json:"name"`
	Root        string     `json:"root"`
	Status      string     `json:"status"`
	WorkspaceID string     `json:"workspace_id"`
	CreatedAt   time.Time  `json:"created_at"`
	LastUsedAt  time.Time  `json:"last_used_at"`
	Checkouts   []Checkout `json:"checkouts"`
}

type State struct {
	Sessions []Session `json:"sessions"`
}

func stateDir() (string, error) {
	if dir := os.Getenv("HERDR_PLUGIN_STATE_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".local", "state", "herdr-utena"), nil
}

func statePath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	return filepath.Join(dir, "sessions.json"), nil
}

func loadState() (*State, error) {
	path, err := statePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &s, nil
}

func saveState(s *State) error {
	path, err := statePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	data = append(data, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (s *State) find(name string) (*Session, bool) {
	for i := range s.Sessions {
		if s.Sessions[i].Name == name {
			return &s.Sessions[i], true
		}
	}
	return nil, false
}

func (s *State) upsert(sess Session) {
	if existing, ok := s.find(sess.Name); ok {
		*existing = sess
		return
	}
	s.Sessions = append(s.Sessions, sess)
}
