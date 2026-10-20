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
	a := sized(t, router.WorkspaceCloneFormView, 120, 30)
	m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	assert.False(t, m.(App).help.full)
}

func TestApp_QuestionMarkOpensHelpInFormPickers(t *testing.T) {
	for _, v := range []router.View{router.SessionFormView, router.AddWorkspaceFormView} {
		a := sized(t, v, 120, 30)
		m, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
		assert.True(t, m.(App).help.full, "view %v", v)
	}
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

func runCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var msgs []tea.Msg
		for _, c := range batch {
			msgs = append(msgs, runCmd(c)...)
		}
		return msgs
	}
	return []tea.Msg{msg}
}

func TestParkFilter_TurnsQuitIntoPark(t *testing.T) {
	assert.Equal(t, ParkMsg{}, ParkFilter(nil, tea.QuitMsg{}))
	key := tea.KeyMsg{Type: tea.KeyEsc}
	assert.Equal(t, key, ParkFilter(nil, key))
}

func TestApp_CtrlCInterruptsWhenParking(t *testing.T) {
	_, cmd := NewApp("0", router.SessionListView, WithPark(func() error { return nil })).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, tea.InterruptMsg{}, cmd())

	_, cmd = NewApp("0", router.SessionListView).Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.Equal(t, tea.QuitMsg{}, cmd())
}

func TestApp_ParkReturnsToSessionListAndParks(t *testing.T) {
	parked := 0
	var m tea.Model = NewApp("0", router.SessionListView, WithPark(func() error {
		parked++
		return nil
	}))
	m, _ = m.Update(router.NavigateToMsg{Target: router.SessionFormView})
	assert.Equal(t, router.SessionFormView, m.(App).router.ActiveView())

	m, cmd := m.Update(ParkMsg{})
	runCmd(cmd)

	assert.Equal(t, router.SessionListView, m.(App).router.ActiveView())
	assert.Equal(t, 1, parked)
}

func TestApp_FocusRefetchesOnlyWhenParking(t *testing.T) {
	_, cmd := NewApp("0", router.SessionListView, WithPark(func() error { return nil })).Update(tea.FocusMsg{})
	assert.NotEmpty(t, runCmd(cmd))
}
