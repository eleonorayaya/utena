package sessionlist

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
)

func TestSessionList_InfoKey_NavigatesToDetail(t *testing.T) {
	m := New()
	m.list.SetItems([]list.Item{
		sessionItem{session: session.Session{Name: "test-session", Status: session.StatusActive}},
	})
	_, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	assert.True(t, handled)
	assert.NotNil(t, cmd)
}
