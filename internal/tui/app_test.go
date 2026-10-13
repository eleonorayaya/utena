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
	m, _ := NewApp("", "0", view).Update(tea.WindowSizeMsg{Width: w, Height: h})
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
	assert.Contains(t, m.View(), "ctrl+g")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotContains(t, m.View(), "ctrl+g")
}

func TestApp_QuestionMarkIsTypedInTextForms(t *testing.T) {
	a := sized(t, router.SessionFormView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.False(t, m.(App).help.full)
}
