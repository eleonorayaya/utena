package workspacecloneform

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ready(t *testing.T, roots ...string) Model {
	t.Helper()
	m, _ := New().Init()
	m.SetSize(80, 20)
	m, _ = m.Update(provider.WorkspaceRootsMsg{Roots: roots})
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestClone_EmptyURLErrorUnderURL(t *testing.T) {
	m := ready(t, "/code")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
	lines := strings.Split(m.View(), "\n")
	assert.Contains(t, lines[2], "URL")
	assert.Contains(t, lines[3], "clone URL is required")
}

func TestClone_SubmitFromDirectoryFieldAndIgnoreRepeats(t *testing.T) {
	m := ready(t, "/code")
	m = typeText(m, "git@github.com:acme/web.git")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "web2")
	assert.Contains(t, m.View(), "/code/web2")
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.cloneWorkspaceIntentMsg", reflect.TypeOf(cmd()).String())
	assert.Equal(t, "cloning…", m.Status().Text)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
}

func TestClone_APIErrorShowsUnderURL(t *testing.T) {
	m := ready(t, "/code")
	m = typeText(m, "x")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(provider.WorkspaceClonedMsg{Err: errors.New("already exists")})
	assert.Contains(t, m.View(), "already exists")
	assert.False(t, m.cloning)
}

func TestClone_RootChoiceOnlyWithMultipleRoots(t *testing.T) {
	assert.NotContains(t, ready(t, "/code").View(), "Root")
	m := ready(t, "/code", "/work")
	assert.Contains(t, m.View(), "Root")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, "/work", m.root.Value())
}
