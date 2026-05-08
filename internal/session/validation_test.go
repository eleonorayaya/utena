package session

import (
	"testing"

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
			name:        "valid session",
			session:     &Session{Name: "valid"},
			expectError: false,
		},
		{
			name:        "nil session",
			session:     nil,
			expectError: true,
			errorMsg:    "cannot be nil",
		},
		{
			name:        "empty fields allowed",
			session:     &Session{},
			expectError: false,
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
		id          uint
		expectError bool
	}{
		{
			name:        "valid ID",
			id:          1,
			expectError: false,
		},
		{
			name:        "zero ID",
			id:          0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionID(tt.id)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "session ID cannot be zero")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestValidateWorkspaceID(t *testing.T) {
	tests := []struct {
		name        string
		id          uint
		expectError bool
	}{
		{
			name:        "valid ID",
			id:          1,
			expectError: false,
		},
		{
			name:        "zero ID",
			id:          0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateWorkspaceID(tt.id)

			if tt.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "workspace ID cannot be zero")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
