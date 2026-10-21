package sessiondetail

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetail_DeletePromptInFooter(t *testing.T) {
	m := focusedOn(makeArchivedSession())
	m, _ = m.Update(runeKey("d"))
	assert.Equal(t, ui.Status{Text: "press d again to delete archived (removes worktrees)", Kind: ui.StatusWarning}, m.Status())
	m, _ = m.Update(runeKey("j"))
	assert.True(t, m.Status().Empty())
}

func TestDetail_BlockerDeletePromptInFooter(t *testing.T) {
	m := focusedOn(makeBlockedSession())
	m, _ = m.Update(runeKey("x"))
	assert.Equal(t, ui.StatusWarning, m.Status().Kind)
	assert.Contains(t, m.Status().Text, "press x again")
}

func TestDetail_CursorMovesIntoPRs(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, Title: "second", State: git.PRStateOpen, HTMLURL: "u1"})
	m := focusedOn(s)
	require.Len(t, m.items(), 2)
	assert.Equal(t, 0, m.cursor)
	assert.Contains(t, lineWith(plainLines(m.View()), "#4847"), "▌ #4847")
	assert.Equal(t, "open PR", m.Keys().ShortHelp()[1].Help().Desc)

	_, cmd := m.Update(runeKey("o"))
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "https://github.com/o/api-service/pull/4847"}, cmd())

	m, _ = m.Update(runeKey("j"))
	assert.Contains(t, lineWith(plainLines(m.View()), "#1 "), "▌ #1")
	_, cmd = m.Update(runeKey("o"))
	require.NotNil(t, cmd)
	assert.Equal(t, provider.OpenURLIntentMsg{SessionID: 1, URL: "u1"}, cmd())
}

func TestDetail_EnterSwitchesEvenOnPR(t *testing.T) {
	s := richSession()
	s.IsAttached = false
	m := focusedOn(s)
	require.NotEqual(t, "", m.selectedPRURL())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.activateSessionMsg", msgType(cmd()))
}

func TestDetail_OpenWithoutTabOrPRShowsStatus(t *testing.T) {
	m := focusedOn(makeBlockedSession())
	m, cmd := m.Update(runeKey("o"))
	assert.Nil(t, cmd)
	assert.Equal(t, "select a tab or pull request to open", m.Status().Text)
}

func TestDetail_PRsWithoutURLAreNotSelectable(t *testing.T) {
	s := makeActiveSession()
	s.PullRequests = []git.PullRequest{{Number: 7, Title: "no link", State: git.PRStateOpen}}
	m := focusedOn(s)
	assert.Equal(t, -1, m.cursor)
	assert.NotContains(t, m.View(), "▌")
}

func TestDetail_CursorStaysOnPRWhenPRsChange(t *testing.T) {
	s := richSession()
	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"})
	m := focusedOn(s)
	m, _ = m.Update(runeKey("j"))
	require.Equal(t, "u1", m.selectedPRURL())

	s.PullRequests = append(s.PullRequests, git.PullRequest{Number: 9000, State: git.PRStateOpen, HTMLURL: "u9000"})
	m = selectIn(m, s)
	assert.Equal(t, "u1", m.selectedPRURL())
}

func TestDetail_FocusPRSelectsFirstOpenablePR(t *testing.T) {
	s := makeBlockedSession()
	s.PullRequests = []git.PullRequest{
		{Number: 1, State: git.PRStateMerged, HTMLURL: "merged"},
		{Number: 2, State: git.PRStateDraft, HTMLURL: "draft"},
	}
	m := New().Select(&s).FocusPR()
	assert.True(t, m.Focused())
	assert.Equal(t, "draft", m.selectedPRURL())
}

func manyPRs(n int) session.Session {
	s := makeActiveSession()
	for i := 1; i <= n; i++ {
		s.PullRequests = append(s.PullRequests, git.PullRequest{Number: i, Title: fmt.Sprintf("pr %d", i), State: git.PRStateOpen, HTMLURL: fmt.Sprintf("u%d", i)})
	}
	return s
}

func TestDetail_ScrollsToKeepCursorVisible(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	m = selectIn(m, manyPRs(20)).Focus()
	for range 15 {
		m, _ = m.Update(runeKey("j"))
	}
	lines := plainLines(m.View())
	require.Len(t, lines, 8)
	assert.Contains(t, lines[0], "my-feature")
	assert.Contains(t, lineWith(lines, "▌"), "pr 5")
	for _, l := range strings.Split(m.View(), "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), 60)
	}

	for range 15 {
		m, _ = m.Update(runeKey("k"))
	}
	lines = plainLines(m.View())
	assert.Contains(t, lineWith(lines, "▌"), "pr 20")
	assert.Equal(t, 0, m.offset)
}

func TestDetail_BlurResetsScroll(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 60, Height: 8})
	m = selectIn(m, manyPRs(20)).Focus()
	for range 15 {
		m, _ = m.Update(runeKey("j"))
	}
	m = m.Blur()
	assert.False(t, m.Focused())
	assert.Equal(t, 0, m.offset)
	assert.NotContains(t, m.View(), "▌")
}

func TestDetail_ShortHelp(t *testing.T) {
	var got []string
	for _, b := range keys.ShortHelp() {
		got = append(got, b.Help().Key+" "+b.Help().Desc)
	}
	assert.Equal(t, []string{"enter switch", "b block", "a archive", "tab/esc sessions"}, got)
}

func TestDetail_SelectingAnotherSessionResetsCursor(t *testing.T) {
	m := focusedOn(makeBlockedSession())
	m, _ = m.Update(runeKey("j"))
	require.Equal(t, 1, m.cursor)
	m = selectIn(m, richSession())
	assert.Equal(t, 0, m.cursor)
	assert.True(t, m.Status().Empty())
}

func TestDetail_CtrlDUJumpBetweenSections(t *testing.T) {
	s := makeBlockedSession()
	s.PullRequests = []git.PullRequest{
		{Number: 1, State: git.PRStateOpen, HTMLURL: "u1"},
		{Number: 2, State: git.PRStateOpen, HTMLURL: "u2"},
	}
	m := focusedOn(s)
	down := tea.KeyMsg{Type: tea.KeyCtrlD}
	up := tea.KeyMsg{Type: tea.KeyCtrlU}

	require.Len(t, m.items(), 4)

	m, _ = m.Update(down)
	assert.Equal(t, 2, m.cursor)
	assert.NotEqual(t, "", m.selectedPRURL())
	m, _ = m.Update(down)
	assert.Equal(t, 2, m.cursor)

	m, _ = m.Update(runeKey("j"))
	require.Equal(t, 3, m.cursor)
	m, _ = m.Update(up)
	assert.Equal(t, 2, m.cursor)
	m, _ = m.Update(up)
	assert.Equal(t, 0, m.cursor)
	m, _ = m.Update(up)
	assert.Equal(t, 0, m.cursor)
}

func TestDetail_CtrlDUJumpBetweenWorkspacePRGroups(t *testing.T) {
	s := makeBlockedSession()
	s.Worktrees = []session.SessionWorktree{wt(0, "api-service", 1, 10), wt(1, "web-app", 2, 20), wt(2, "empty", 3, 30)}
	s.PullRequests = []git.PullRequest{
		{Number: 1, RepoID: 1, State: git.PRStateOpen, HTMLURL: "api1"},
		{Number: 2, RepoID: 1, State: git.PRStateOpen, HTMLURL: "api2"},
		{Number: 3, RepoID: 2, State: git.PRStateOpen, HTMLURL: "web3"},
		{Number: 4, RepoID: 9, State: git.PRStateOpen, HTMLURL: "other4"},
	}
	m := focusedOn(s)
	down := tea.KeyMsg{Type: tea.KeyCtrlD}
	up := tea.KeyMsg{Type: tea.KeyCtrlU}

	var visited []string
	for range 4 {
		m, _ = m.Update(down)
		visited = append(visited, m.selectedPRURL())
	}
	assert.Equal(t, []string{"api2", "web3", "other4", "other4"}, visited)

	visited = nil
	for range 3 {
		m, _ = m.Update(up)
		visited = append(visited, m.selectedPRURL())
	}
	assert.Equal(t, []string{"web3", "api2", ""}, visited)
	assert.Equal(t, 0, m.cursor)
}

func TestDetail_CursorFollowsPRAcrossWorkspaceGroups(t *testing.T) {
	s := makeActiveSession()
	s.Worktrees = []session.SessionWorktree{wt(0, "api-service", 1, 10)}
	s.PullRequests = []git.PullRequest{
		{Number: 1, RepoID: 1, State: git.PRStateOpen, HTMLURL: "a"},
		{Number: 2, RepoID: 9, State: git.PRStateOpen, HTMLURL: "b"},
	}
	m := focusedOn(s)
	m, _ = m.Update(runeKey("j"))
	require.Equal(t, "b", m.selectedPRURL())

	s.Worktrees = []session.SessionWorktree{wt(0, "web-app", 9, 20), wt(1, "api-service", 1, 10)}
	m = selectIn(m, s)
	assert.Equal(t, 0, m.cursor)
	assert.Equal(t, "b", m.selectedPRURL())
}
