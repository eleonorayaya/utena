package filepicker

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilePicker_ListsDirsAndNavigates(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"beta", "alpha", ".hidden", "alpha/inner"} {
		require.NoError(t, os.MkdirAll(filepath.Join(root, d), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(root, "file.txt"), nil, 0o644))

	m := NewAt(root)
	m.SetSize(40, 10)
	m, _ = m.Init()
	out := m.View()
	assert.Contains(t, out, "▸ alpha")
	assert.Contains(t, out, "▸ beta")
	assert.NotContains(t, out, "file.txt")
	assert.NotContains(t, out, ".hidden")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, filepath.Join(root, "alpha"), m.Dir())
	assert.Contains(t, m.View(), "inner")

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, root, m.Dir())
	assert.True(t, strings.Contains(m.View(), "▌ ▸ alpha"))

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	assert.Contains(t, m.View(), ".hidden")

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	assert.Equal(t, DirectorySelectedMsg{Path: root}, cmd())
}
