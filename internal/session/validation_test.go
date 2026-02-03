package session

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateSession(t *testing.T) {
	tests := []struct {
		name        string
		session     *Session
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid session",
			session: &Session{
				ID:          "session-1",
				WorkspaceID: "ws-1",
				LastUsedAt:  time.Now(),
			},
			expectError: false,
		},
		{
			name:        "nil session",
			session:     nil,
			expectError: true,
			errorMsg:    "cannot be nil",
		},
		{
			name: "empty ID",
			session: &Session{
				WorkspaceID: "ws-1",
				LastUsedAt:  time.Now(),
			},
			expectError: true,
			errorMsg:    "session name cannot be empty",
		},
		{
			name: "empty WorkspaceID",
			session: &Session{
				ID:         "session-1",
				LastUsedAt: time.Now(),
			},
			expectError: false,
		},
		{
			name: "zero LastUsedAt",
			session: &Session{
				ID:          "session-1",
				WorkspaceID: "ws-1",
			},
			expectError: true,
			errorMsg:    "LastUsedAt cannot be zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSession(tt.session)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSessionName(t *testing.T) {
	tests := []struct {
		name        string
		sessionName string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "valid alphanumeric",
			sessionName: "my-session-1",
			expectError: false,
		},
		{
			name:        "valid with underscores",
			sessionName: "my_session_2",
			expectError: false,
		},
		{
			name:        "valid single character",
			sessionName: "a",
			expectError: false,
		},
		{
			name:        "empty name",
			sessionName: "",
			expectError: true,
			errorMsg:    "session name cannot be empty",
		},
		{
			name:        "contains spaces",
			sessionName: "my session",
			expectError: true,
			errorMsg:    "contains invalid characters",
		},
		{
			name:        "contains dots",
			sessionName: "my.session",
			expectError: true,
			errorMsg:    "contains invalid characters",
		},
		{
			name:        "contains special characters",
			sessionName: "my@session!",
			expectError: true,
			errorMsg:    "contains invalid characters",
		},
		{
			name:        "exceeds 50 characters",
			sessionName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			expectError: true,
			errorMsg:    "cannot exceed 50 characters",
		},
		{
			name:        "exactly 50 characters",
			sessionName: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionName(tt.sessionName)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateSessionID(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		expectError bool
	}{
		{
			name:        "valid ID",
			id:          "session-1",
			expectError: false,
		},
		{
			name:        "empty ID",
			id:          "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionID(tt.id)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "ID cannot be empty")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateWorkspaceID(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		expectError bool
	}{
		{
			name:        "valid ID",
			id:          "ws-1",
			expectError: false,
		},
		{
			name:        "empty ID",
			id:          "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkspaceID(tt.id)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "ID cannot be empty")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
