package sessiondetail

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetail_DeletePromptInFooter(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeArchivedSession()})
	m, _ = m.Update(runeKey("d"))
	assert.Equal(t, ui.Status{Text: "press d again to delete archived (removes worktrees)", Kind: ui.StatusWarning}, m.Status())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.True(t, m.Status().Empty())
}

func TestDetail_BlockerDeletePromptInFooter(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("x"))
	assert.Equal(t, ui.StatusWarning, m.Status().Kind)
	assert.Contains(t, m.Status().Text, "press x again")
}

func TestDetail_OpenPR(t *testing.T) {
	s := richSession()
	m, _ := New().Update(SelectMsg{Session: s})
	_, cmd := m.Update(runeKey("o"))
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "https://github.com/o/api-service/pull/4847"}, cmd())

	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ = New().Update(SelectMsg{Session: s})
	m, cmd = m.Update(runeKey("o"))
	assert.Nil(t, cmd)
	require.True(t, m.picker.IsOpen())
	m, _ = m.Update(runeKey("j"))
	assert.Equal(t, -1, m.cursor)
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "https://github.com/o/api-service/pull/4847"}, cmd())
}

func TestDetail_EscClosesPickerBeforeGoingBack(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.False(t, m.picker.IsOpen())
}

func TestDetail_PickerRendersInView(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, Title: "second", State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	out := m.View()
	assert.Len(t, strings.Split(out, "\n"), 20)
	assert.Contains(t, out, "second")
}

func TestDetail_OpenURLFailed(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	m, _ = m.Update(ui.OpenURLFailedMsg{Err: errors.New("boom")})
	assert.Equal(t, ui.StatusError, m.Status().Kind)
}

func TestDetail_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter switch", "o open PR", "b block", "a archive", "esc back"}, got)
}

func TestDetail_PickerClosesWhenSessionPRsChange(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	require.True(t, m.picker.IsOpen())

	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{s}})
	require.True(t, m.picker.IsOpen())

	s.PullRequests[1].State = git.PRStateMerged
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{s}})
	assert.False(t, m.picker.IsOpen())
}

func TestDetail_PickerClosesWhenSessionDisappears(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m, _ := New().Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("o"))
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{})
	assert.False(t, m.picker.IsOpen())
}

func TestDetail_QuitsAfterSessionSwitch(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: richSession()})
	_, cmd := m.Update(provider.SessionSwitchedMsg{})
	require.NotNil(t, cmd)
	assert.Equal(t, tea.QuitMsg{}, cmd())
}
