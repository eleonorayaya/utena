package sessionlist

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var escKey = tea.KeyMsg{Type: tea.KeyEsc}

func escQuitList(t *testing.T, ss ...session.Session) Model {
	t.Helper()
	m, _ := New(WithEscQuits()).Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: ss})
	return m
}

func TestEsc_DoesNotQuitByDefault(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle))

	_, cmd, _ := m.OnKeyMsg(escKey)

	assert.Nil(t, cmd)
}

func TestEsc_QuitsWithEscQuits(t *testing.T) {
	m := escQuitList(t, withClaude(1, "a", claude.StatusIdle))

	_, cmd, handled := m.OnKeyMsg(escKey)

	assert.True(t, handled)
	require.NotNil(t, cmd)
	assert.Equal(t, tea.QuitMsg{}, cmd())
}

func TestEsc_BlursFocusedDetailBeforeQuitting(t *testing.T) {
	m := escQuitList(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = press(m, tab())
	require.True(t, m.detail.Focused())

	m, cmd := press(m, escKey)

	assert.NotContains(t, uitest.Drain(cmd), tea.Msg(tea.QuitMsg{}))
	assert.False(t, feed(m, cmd).detail.Focused())
}

func TestEscQuits_HelpShowsEsc(t *testing.T) {
	assert.Equal(t, "q/esc", New(WithEscQuits()).keys().Quit.Help().Key)
	assert.Equal(t, "q", New().keys().Quit.Help().Key)
}
