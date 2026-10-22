package sessionpicker

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sessions() []session.Session {
	now := time.Now()
	return []session.Session{
		{Name: "old", Status: session.StatusActive, LastUsedAt: now.Add(-time.Hour)},
		{Name: "recent", Status: session.StatusInactive, LastUsedAt: now},
		{Name: "attached", Status: session.StatusActive, IsAttached: true, LastUsedAt: now.Add(-2 * time.Hour)},
	}
}

func opened() Model {
	m := New("Move tab to")
	m.SetSize(60, 20)
	return m.Open(sessions())
}

func TestPicker_ListsAttachedFirstThenMostRecent(t *testing.T) {
	m := opened()
	var names []string
	for _, it := range m.list.Items() {
		names = append(names, it.(sessionItem).session.Name)
	}
	assert.Equal(t, []string{"attached", "recent", "old"}, names)
	assert.Contains(t, m.View(), "Move tab to")
}

func TestPicker_EnterSelectsHighlightedSession(t *testing.T) {
	m := opened()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg, ok := cmd().(SelectedMsg)
	require.True(t, ok)
	assert.Equal(t, "recent", msg.Session.Name)
}

func TestPicker_EscCloses(t *testing.T) {
	_, cmd := opened().Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	assert.Equal(t, ClosedMsg{}, cmd())
}

func TestPicker_EmptyListSaysSo(t *testing.T) {
	m := New("Move tab to")
	m.SetSize(60, 20)
	m = m.Open(nil)
	assert.Contains(t, m.View(), "No other sessions")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		_, isSelect := cmd().(SelectedMsg)
		assert.False(t, isSelect)
	}
}
