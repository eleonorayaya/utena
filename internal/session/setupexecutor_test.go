package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInstallCommand(t *testing.T) {
	tests := []struct {
		name     string
		pm       string
		runWith  []string
		wantName string
		wantArgs []string
	}{
		{"runs package manager directly", "yarn", nil, "yarn", []string{"install"}},
		{"prefixes run_with", "yarn", []string{"mise", "exec", "--"}, "mise", []string{"exec", "--", "yarn", "install"}},
		{"single-word run_with", "npm", []string{"nice"}, "nice", []string{"npm", "install"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			name, args := installCommand(tc.pm, tc.runWith)
			require.Equal(t, tc.wantName, name)
			require.Equal(t, tc.wantArgs, args)
		})
	}
}
