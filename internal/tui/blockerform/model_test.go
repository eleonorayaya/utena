package blockerform

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func started(t *testing.T) Model {
	t.Helper()
	m, _ := New().Init()
	m, _ = m.Update(StartMsg{SessionID: 4})
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestBlockerForm_CyclesKind(t *testing.T) {
	m := started(t)
	assert.Equal(t, session.BlockerKindReview, m.kind())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, session.BlockerKindSlack, m.kind())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	assert.Equal(t, session.BlockerKindOther, m.kind())
}

func TestBlockerForm_SubmitSendsAddBlocker(t *testing.T) {
	m := started(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "waiting on sam")
	assert.Contains(t, m.View(), "waiting on sam")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.addBlockerIntentMsg", reflect.TypeOf(cmd()).String())
}

func TestBlockerForm_SubmitWithEverythingEmptyIsAllowed(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
}

func TestBlockerForm_EscGoesBack(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	assert.IsType(t, router.BackMsg{}, cmd())
}

func TestBlockerForm_AddedGoesBack(t *testing.T) {
	m := started(t)
	_, cmd := m.Update(provider.BlockerAddedMsg{SessionID: 4})
	require.NotNil(t, cmd)
	assert.IsType(t, router.BackMsg{}, cmd())
}

func TestBlockerForm_ArrowsInTextFieldDoNotChangeKind(t *testing.T) {
	m := started(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRight})
	assert.Equal(t, session.BlockerKindReview, m.kind())
}

func TestBlockerForm_VimKeysCycleKind(t *testing.T) {
	m := started(t)
	m = typeText(m, "l")
	assert.Equal(t, session.BlockerKindSlack, m.kind())
	m = typeText(m, "hh")
	assert.Equal(t, session.BlockerKindOther, m.kind())
}

func TestBlockerForm_VimKeysTypeInTextFields(t *testing.T) {
	m := started(t)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "hl")
	assert.Equal(t, session.BlockerKindReview, m.kind())
	assert.Equal(t, "hl", m.note.Value())
}
