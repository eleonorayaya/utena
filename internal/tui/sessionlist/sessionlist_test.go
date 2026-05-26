package sessionlist

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestSessionList_InfoKey_NavigatesToDetail(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Name: "test-session", Status: session.StatusActive},
	}
	_, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("i")})
	assert.True(t, handled)
	assert.NotNil(t, cmd)
}

func TestSessionList_Delete_Creating_FirstPress_ShowsForceMessage(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "stuck", Status: session.StatusCreating},
	}

	result, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(1), result.pendingDeleteID)
	assert.Contains(t, result.statusMsg, "force delete stuck")
}

func TestSessionList_Delete_Creating_SecondPress_ForceDeletes(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "stuck", Status: session.StatusCreating},
	}
	m.pendingDeleteID = 1

	result, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.True(t, handled)
	assert.NotNil(t, cmd)
	assert.Equal(t, uint(0), result.pendingDeleteID)
}
