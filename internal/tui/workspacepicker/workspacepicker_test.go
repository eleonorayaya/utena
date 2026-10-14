package workspacepicker

import (
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ws(id uint, name string, ago time.Duration) workspace.Workspace {
	w := workspace.Workspace{Name: name, IsGitRepo: true, IsBare: true, LastUsedAt: time.Now().Add(-ago)}
	w.ID = id
	return w
}

func picker(multi bool, wss ...workspace.Workspace) Model {
	m := New("Workspaces", multi)
	m.SetSize(50, 10)
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: wss})
	return m.Open(nil)
}

func TestPicker_OneLineRowsSortedByLastUsed(t *testing.T) {
	m := picker(false, ws(1, "old", 48*time.Hour), ws(2, "new", time.Hour))
	lines := strings.Split(m.View(), "\n")
	assert.Contains(t, lines[0], "Workspaces")
	assert.True(t, strings.HasPrefix(lines[2], "▌ new"))
	assert.Contains(t, lines[2], "bare")
	assert.True(t, strings.HasSuffix(strings.TrimRight(lines[2], " "), "1h"))
	assert.True(t, strings.HasPrefix(lines[3], "  old"))
	for _, l := range lines {
		assert.LessOrEqual(t, lipgloss.Width(l), 50)
	}
}

func TestPicker_MultiSelectCheckboxesAndConfirm(t *testing.T) {
	m := picker(true, ws(1, "a", time.Hour), ws(2, "b", 2*time.Hour))
	assert.Contains(t, m.View(), "[ ] a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	assert.Contains(t, m.View(), "[x] a")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	sel := cmd().(SelectedMsg)
	assert.Equal(t, []string{"a", "b"}, []string{sel.Workspaces[0].Name, sel.Workspaces[1].Name})
}

func TestPicker_EnterWithoutToggleSelectsCursor(t *testing.T) {
	m := picker(true, ws(1, "a", time.Hour))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, "a", cmd().(SelectedMsg).Workspaces[0].Name)
}

func TestPicker_OpenRestoresSelection(t *testing.T) {
	m := picker(true, ws(1, "a", time.Hour), ws(2, "b", 2*time.Hour)).Open([]uint{2})
	assert.Contains(t, m.View(), "[x] b")
	assert.Contains(t, m.View(), "[ ] a")
}

func TestKindSpan(t *testing.T) {
	assert.Equal(t, "not git", KindSpan(workspace.Workspace{}).Text)
	assert.Equal(t, "not bare", KindSpan(workspace.Workspace{IsGitRepo: true}).Text)
	assert.Equal(t, "◌ cloning…", KindSpan(workspace.Workspace{Status: workspace.StatusCloning}).Text)
	assert.Equal(t, "✕ failed", KindSpan(workspace.Workspace{Status: workspace.StatusFailed}).Text)
}

func TestPicker_EscClosesOnlyWhenNoFilter(t *testing.T) {
	m := picker(false, ws(1, "alpha", time.Hour), ws(2, "beta", 2*time.Hour))
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, []tea.Msg{ClosedMsg{}}, uitest.Drain(cmd))

	m, _ = m.Update(uitest.Runes("/"))
	m, _ = m.Update(uitest.Runes("b"))
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotContains(t, uitest.Drain(cmd), tea.Msg(ClosedMsg{}))
	assert.False(t, m.CapturesText())
}
