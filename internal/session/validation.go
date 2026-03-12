package session

import (
	"errors"
	"fmt"
	"regexp"
)

var validSessionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func ValidateSessionName(name string) error {
	if name == "" {
		return errors.New("session name cannot be empty")
	}

	if len(name) > 50 {
		return fmt.Errorf("session name cannot exceed 50 characters (got %d)", len(name))
	}

	if !validSessionNamePattern.MatchString(name) {
		return fmt.Errorf("session name '%s' contains invalid characters (only alphanumeric, hyphens, and underscores are allowed)", name)
	}

	return nil
}

func ValidateSession(session *Session) error {
	if session == nil {
		return errors.New("session cannot be nil")
	}
	return nil
}

func ValidateSessionID(id uint) error {
	if id == 0 {
		return errors.New("session ID cannot be zero")
	}

	return nil
}

func ValidateWorkspaceID(id uint) error {
	if id == 0 {
		return errors.New("workspace ID cannot be zero")
	}

	return nil
}
