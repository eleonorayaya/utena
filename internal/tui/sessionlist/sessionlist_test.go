package sessionlist

import (
	"reflect"
	"strings"
	"testing"
	"time"

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

func pressA(m Model) (Model, tea.Cmd, bool) {
	return m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
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

func TestSessionList_Archive_Active_FirstPress_ShowsArchiveMessage(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}

	result, cmd, handled := pressA(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(1), result.pendingArchiveID)
	assert.Contains(t, result.statusMsg, "archive live")
}

func TestSessionList_Archive_Active_SecondPress_Archives(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}
	m.pendingArchiveID = 1

	result, cmd, handled := pressA(m)
	assert.True(t, handled)
	assert.Equal(t, "provider.archiveSessionIntentMsg", msgTypeName(cmd))
	assert.Equal(t, uint(0), result.pendingArchiveID)
}

func TestSessionList_Archive_AlreadyArchived_NoOp(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "old", Status: session.StatusArchived},
	}

	result, cmd, handled := pressA(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(0), result.pendingArchiveID)
	assert.Contains(t, result.statusMsg, "already archived")
}

func TestSessionList_Delete_Active_FirstPress_ShowsDeleteMessage(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(1), result.pendingDeleteID)
	assert.Contains(t, result.statusMsg, "delete live")
}

func TestSessionList_Delete_Active_SecondPress_ForceDeletes(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
	}
	m.pendingDeleteID = 1

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Equal(t, "provider.deleteSessionIntentMsg", msgTypeName(cmd))
	assert.Equal(t, uint(0), result.pendingDeleteID)
}

func TestSessionList_Delete_Attached_Blocked(t *testing.T) {
	m := New()
	m.filtered = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive, IsAttached: true},
	}

	result, cmd, handled := pressD(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, uint(0), result.pendingDeleteID)
	assert.Contains(t, result.statusMsg, "cannot delete attached")
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

func TestSessionList_SortsHiddenAfterActive(t *testing.T) {
	m := New()
	m.showHidden = true
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "old", Status: session.StatusArchived},
		{Model: gorm.Model{ID: 2}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 3}, Name: "bad", Status: session.StatusBroken},
		{Model: gorm.Model{ID: 4}, Name: "live2", Status: session.StatusActive},
	}
	m.rebuildFiltered()

	var names []string
	for _, s := range m.filtered {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"live", "live2", "old", "bad"}, names)
}

func TestSessionList_ToggleHidden_RevealsArchivedAndBroken(t *testing.T) {
	m := New()
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "old", Status: session.StatusArchived},
		{Model: gorm.Model{ID: 3}, Name: "bad", Status: session.StatusBroken},
	}
	m.rebuildFiltered()

	result, _, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(".")})
	assert.True(t, handled)
	assert.True(t, result.showHidden)
	assert.Len(t, result.filtered, 3)
}

func openBlocker(kind session.BlockerKind) session.SessionBlocker {
	return session.SessionBlocker{Kind: kind}
}

func TestSessionList_GroupsBlockedBetweenActiveAndHidden(t *testing.T) {
	m := New()
	m.showHidden = true
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "blocked1", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindReview)}},
		{Model: gorm.Model{ID: 2}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 3}, Name: "old", Status: session.StatusArchived, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindSlack)}},
		{Model: gorm.Model{ID: 4}, Name: "live2", Status: session.StatusInactive},
	}
	m.rebuildFiltered()

	var names []string
	for _, s := range m.filtered {
		names = append(names, s.Name)
	}
	assert.Equal(t, []string{"live", "live2", "blocked1", "old"}, names)
}

func TestSessionList_ResolvedBlockersAreNotBlocked(t *testing.T) {
	now := time.Now()
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "was-blocked", Status: session.StatusActive, Blockers: []session.SessionBlocker{{Kind: session.BlockerKindReview, ResolvedAt: &now}}},
		{Model: gorm.Model{ID: 2}, Name: "live", Status: session.StatusActive},
	}
	m.rebuildFiltered()
	assert.Equal(t, "was-blocked", m.filtered[0].Name)
	assert.NotContains(t, m.renderList(60), "Blocked")
}

func TestSessionList_RendersBlockedHeaderAndTag(t *testing.T) {
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "waiting", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindSlack), openBlocker(session.BlockerKindReview)}},
	}
	m.rebuildFiltered()
	out := m.renderList(60)
	assert.Contains(t, out, "Blocked")
	assert.Contains(t, out, "⏸ slack ×2")
	assert.Less(t, strings.Index(out, "live"), strings.Index(out, "Blocked"))
	assert.Less(t, strings.Index(out, "Blocked"), strings.Index(out, "waiting"))
}

func TestSessionList_CursorMovesAcrossHeader(t *testing.T) {
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "waiting", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindOther)}},
	}
	m.rebuildFiltered()
	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.Equal(t, "waiting", m.selectedSession().Name)
}

func TestSessionList_BlockKey_NavigatesToBlockerForm(t *testing.T) {
	m := New()
	m.filtered = []session.Session{{Model: gorm.Model{ID: 5}, Name: "s", Status: session.StatusActive}}
	_, cmd, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
	assert.True(t, handled)
	assert.NotNil(t, cmd)
}

func TestSessionList_ViewFitsHeightWithBlockedHeaderMidWindow(t *testing.T) {
	m := New()
	m.width, m.height = 60, 10
	m.sessions = []session.Session{{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive}}
	for i := uint(2); i <= 14; i++ {
		m.sessions = append(m.sessions, session.Session{Model: gorm.Model{ID: i}, Name: "b", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindOther)}})
	}
	m.rebuildFiltered()
	assert.LessOrEqual(t, strings.Count(m.View(), "\n")+1, m.height)
}
