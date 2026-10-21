package sessionlist

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pressO(m Model) (Model, tea.Cmd, bool) {
	return m.OnKeyMsg(uitest.Runes("o"))
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
	assert.False(t, m.detail.Focused())
}

func TestOpenPR_EmptyURLIsNotOpenable(t *testing.T) {
	m := listWith(t, withPRs(withClaude(1, "a", claude.StatusIdle), git.PullRequest{Number: 1, State: git.PRStateOpen}))
	m, cmd, _ := pressO(m)
	assert.Nil(t, cmd)
	assert.Equal(t, "no open pull requests", m.Status().Text)
}

func TestOpenPR_SeveralFocusesDetailOnFirstPR(t *testing.T) {
	m := listWith(t, withPRs(withClaude(7, "a", claude.StatusIdle),
		git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		git.PullRequest{Number: 2, State: git.PRStateDraft, HTMLURL: "u2"}))
	m, cmd, handled := pressO(m)
	assert.True(t, handled)
	assert.Nil(t, cmd)
	require.True(t, m.detail.Focused())

	m, cmd = m.Update(uitest.Runes("o"))
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 7, URL: "u1"}, cmd())

	m, _ = m.Update(uitest.Runes("j"))
	assert.Equal(t, "a", m.selectedSession().Name)
	_, cmd = m.Update(uitest.Runes("o"))
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 7, URL: "u2"}, cmd())
}

func TestOpenURLFailed_ShowsError(t *testing.T) {
	m := listWith(t, withClaude(1, "a", claude.StatusIdle))
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.Status{Text: "could not open PR: boom", Kind: ui.StatusError}, m.Status())
}
