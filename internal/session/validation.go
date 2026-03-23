package session

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var validSessionNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

var invalidCharsPattern = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)
var multiDash = regexp.MustCompile(`-{2,}`)

func SanitizeSessionName(name string) string {
	s := strings.ToLower(name)
	s = invalidCharsPattern.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 50 {
		s = strings.TrimRight(s[:50], "-")
	}
	if s == "" {
		return "session"
	}
	return s
}

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
