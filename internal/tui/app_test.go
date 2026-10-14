package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func sized(t *testing.T, view router.View, w, h int) App {
	t.Helper()
	m, _ := NewApp("0", view).Update(tea.WindowSizeMsg{Width: w, Height: h})
	return m.(App)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, " \n"), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func TestApp_FooterIsVisibleAndViewFitsWindow(t *testing.T) {
	a := sized(t, router.SessionListView, 200, 40)
	m, _ := a.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "one", Status: session.StatusActive},
	}})
	out := m.View()
	assert.Equal(t, 40, strings.Count(out, "\n")+1)
	assert.Contains(t, lastLine(out), "? more")
}

func TestApp_QuestionMarkTogglesFullHelp(t *testing.T) {
	a := sized(t, router.SessionListView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.Contains(t, m.View(), "? or esc to close")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotContains(t, m.View(), "? or esc to close")
}

func TestApp_QuestionMarkIsTypedInTextForms(t *testing.T) {
	a := sized(t, router.SessionFormView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.False(t, m.(App).help.full)
}

func TestApp_QuestionMarkIsTypedInAddWorkspaceForm(t *testing.T) {
	a := sized(t, router.AddWorkspaceFormView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.False(t, m.(App).help.full)
}

func TestApp_TinySplitWindowDoesNotPanic(t *testing.T) {
	for _, h := range []int{1, 2, 3, 4} {
		a := sized(t, router.SessionListView, 120, h)
		m, _ := a.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{
			{Model: gorm.Model{ID: 1}, Name: "one", Status: session.StatusActive},
		}})
		assert.NotPanics(t, func() { _ = m.View() })
	}
}

func TestApp_FullHelpFitsShortWindow(t *testing.T) {
	a := sized(t, router.SessionListView, 120, 8)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.LessOrEqual(t, strings.Count(m.View(), "\n")+1, 8)
}

func TestApp_OtherViewsFitWindowWithFooter(t *testing.T) {
	for _, v := range []router.View{router.WorkspaceListView, router.WorkspaceCloneFormView, router.BlockerFormView} {
		a := sized(t, v, 120, 30)
		out := a.View()
		assert.LessOrEqual(t, strings.Count(out, "\n")+1, 30, "view %v", v)
	}
}
