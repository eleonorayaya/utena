package sessionlist

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pressO(m Model) (Model, tea.Cmd, bool) {
	return m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
}

func withPRs(s session.Session, prs ...git.PullRequest) session.Session {
	s.PullRequests = prs
	return s
}

func listWith(t *testing.T, ss ...session.Session) Model {
	t.Helper()
	m, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 20})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: ss})
	return m
}

func splitLines(s string) []string {
	return strings.Split(s, "\n")
}

func TestOpenPR_NoneShowsStatus(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateMerged, HTMLURL: "u"}))
	m, cmd, handled := pressO(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	assert.Equal(t, "no open pull requests", m.Status().Text)
}

func TestOpenPR_SingleOpensDirectly(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"}))
	m, cmd, handled := pressO(m)
	assert.True(t, handled)
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "u1"}, cmd())
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_EmptyURLIsNotOpenable(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateOpen}))
	m, cmd, _ := pressO(m)
	assert.Nil(t, cmd)
	assert.Equal(t, "no open pull requests", m.Status().Text)
}

func TestOpenPR_SeveralOpensPickerThenChooses(t *testing.T) {
	m := listWith(t, withPRs(withClaude(7, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateDraft, HTMLURL: "u2"}))
	m, cmd, _ := pressO(m)
	assert.Nil(t, cmd)
	require.True(t, m.picker.IsOpen())
	assert.Contains(t, m.Status().Text, "enter open")

	m, _, handled := m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	assert.True(t, handled)
	assert.Equal(t, "a", m.selectedSession().Name)

	m, cmd, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 7, URL: "u2"}, cmd())
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_PickerClosesWhenSelectionChanges(t *testing.T) {
	a := withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateOpen, HTMLURL: "u2"})
	b := withClaude(2, "b", claude.StatusIdle)
	m := listWith(t, a, b)
	m, _, _ = pressO(m)
	require.True(t, m.picker.IsOpen())

	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{b}})
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_PickerReservesRowsInView(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 4847, Title: "first", State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, Title: "second", State: git.PRStateDraft, HTMLURL: "u2"}))
	m, _, _ = pressO(m)
	out := m.View()
	assert.Contains(t, out, "#4847")
	assert.LessOrEqual(t, len(splitLines(out)), 20)
}

func TestOpenURLFailed_ShowsError(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.Status{Text: "could not open PR: boom", Kind: ui.StatusError}, m.Status())
}

func TestOpenPR_PickerClosesWhenSelectedSessionPRsChange(t *testing.T) {
	a := withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateOpen, HTMLURL: "u2"})
	m := listWith(t, a)
	m, _, _ = pressO(m)
	require.True(t, m.picker.IsOpen())

	a.PullRequests[1].State = git.PRStateClosed
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{a}})
	assert.False(t, m.picker.IsOpen())
}

func TestOpenPR_PickerRowsVisibleInTinyPopup(t *testing.T) {
	s := withPRs(withClaude(1, "a", claude.StatusIdle),
		git.PullRequest{Number: 41, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 42, State: git.PRStateOpen, HTMLURL: "u2"},
		git.PullRequest{Number: 43, State: git.PRStateOpen, HTMLURL: "u3"})
	m, _ := New().Update(tea.WindowSizeMsg{Width: 40, Height: 2})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{s}})
	m, _, _ = pressO(m)
	out := m.View()
	assert.Equal(t, 2, len(splitLines(out)))
	assert.Contains(t, out, "#43")
}

func TestOpenPR_OffsetReclampsAfterPickerCloses(t *testing.T) {
	a := withClaude(1, "a", claude.StatusIdle)
	b := withClaude(2, "b", claude.StatusIdle)
	c := withPRs(withClaude(3, "c", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateOpen, HTMLURL: "u2"},
		git.PullRequest{Number: 3, State: git.PRStateOpen, HTMLURL: "u3"})
	m, _ := New().Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{a, b, c}})
	for range 2 {
		m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	}
	m, _, _ = pressO(m)
	require.Greater(t, m.offset, 0)

	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, 0, m.offset)
}
