package sessionlist

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func msgTypeName(cmd tea.Cmd) string {
	if cmd == nil {
		return ""
	}
	return reflect.TypeOf(cmd()).String()
}

func pressD(m Model) (Model, tea.Cmd, bool) {
	return m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
}

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

func TestSessionList_Delete_Active_FirstPress_ShowsArchiveMessage(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(1), result.pendingDeleteID)
	assert.Contains(t, result.statusMsg, "archive live")
}

func TestSessionList_Delete_Active_SecondPress_Archives(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}
	m.pendingDeleteID = 1

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Equal(t, "provider.archiveSessionIntentMsg", msgTypeName(cmd))
	assert.Equal(t, uint(0), result.pendingDeleteID)
}

func TestSessionList_Delete_Archived_FirstPress_ShowsDeleteMessage(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "old", Status: session.StatusArchived},
	}

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(1), result.pendingDeleteID)
	assert.Contains(t, result.statusMsg, "delete old")
}

func TestSessionList_Delete_Archived_SecondPress_Deletes(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "old", Status: session.StatusArchived},
	}
	m.pendingDeleteID = 1

	_, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Equal(t, "provider.deleteSessionIntentMsg", msgTypeName(cmd))
}

func TestSessionList_Pending_Dismisses(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "pend", Status: session.StatusPending},
	}

	_, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Equal(t, "provider.dismissSessionIntentMsg", msgTypeName(cmd))
}

func TestSessionList_FiltersArchivedByDefault(t *testing.T) {
	m := New()
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "old", Status: session.StatusArchived},
	}
	m.rebuildFiltered()

	assert.Len(t, m.filtered, 1)
	assert.Equal(t, "live", m.filtered[0].Name)
}

func TestSessionList_ToggleArchived_RevealsArchived(t *testing.T) {
	m := New()
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "old", Status: session.StatusArchived},
	}
	m.rebuildFiltered()

	result, _, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.True(t, handled)
	assert.True(t, result.showArchived)
	assert.Len(t, result.filtered, 2)
}
