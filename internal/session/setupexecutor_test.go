package session

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallCommand(t *testing.T) {
	found := func(string) (string, error) { return "/bin/mise", nil }
	missing := func(string) (string, error) { return "", errors.New("not found") }

	tests := []struct {
		name     string
		pm       string
		lookPath func(string) (string, error)
		wantName string
		wantArgs []string
	}{
		{"wraps bare package manager in mise exec", "yarn", found, "/bin/mise", []string{"exec", "--", "yarn", "install"}},
		{"runs package manager directly without mise", "yarn", missing, "yarn", []string{"install"}},
		{"runs package manager path directly", "/opt/pm/yarn", found, "/opt/pm/yarn", []string{"install"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args := installCommand(tc.pm, tc.lookPath)
			require.Equal(t, tc.wantName, name)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}
