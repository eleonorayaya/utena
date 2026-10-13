package theme

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefault_StatusTokensFallBackToExistingColours(t *testing.T) {
	d := Default()
	assert.Equal(t, d.Primary, d.StatusAttention)
	assert.Equal(t, d.AccentMint, d.StatusReview)
	assert.Equal(t, d.AccentLavender, d.StatusWorking)
	assert.Equal(t, d.TextMuted, d.StatusDone)
	assert.Equal(t, d.TextMuted, d.StatusIdle)
}

func TestLoad_OverridesStatusTokensAndKeepsOthers(t *testing.T) {
	t.Cleanup(func() { Current = Default() })
	path := filepath.Join(t.TempDir(), "theme.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"status_review":"#00ff00","primary":"#111111"}`), 0o644))

	require.NoError(t, Load(path))

	assert.Equal(t, lipgloss.Color("#00ff00"), Current.StatusReview)
	assert.Equal(t, lipgloss.Color("#111111"), Current.Primary)
	assert.Equal(t, Default().AccentLavender, Current.StatusWorking)
}

func TestLoad_StatusTokensInheritOverriddenBaseColours(t *testing.T) {
	t.Cleanup(func() { Current = Default() })
	path := filepath.Join(t.TempDir(), "theme.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"primary":"#123456"}`), 0o644))

	require.NoError(t, Load(path))

	assert.Equal(t, lipgloss.Color("#123456"), Current.StatusAttention)
}
