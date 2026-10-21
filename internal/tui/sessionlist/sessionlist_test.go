package sessionlist

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSessionList_PreviewShowsEmbeddedPRsForMultiWorkspaceSession(t *testing.T) {
	utenaBranch, dotfilesBranch := uint(1), uint(2)
	sess := session.Session{
		Model:  gorm.Model{ID: 7},
		Name:   "multi",
		Status: session.StatusActive,
		Worktrees: []session.SessionWorktree{
			{Worktree: &git.Worktree{BranchID: utenaBranch}, Workspace: &workspace.Workspace{Model: gorm.Model{ID: 10}, Name: "utena"}},
			{Worktree: &git.Worktree{BranchID: dotfilesBranch}, Workspace: &workspace.Workspace{Model: gorm.Model{ID: 20}, Name: "dotfiles"}},
		},
		PullRequests: []git.PullRequest{
			{Number: 116, Title: "utena pr", HeadBranchID: &utenaBranch, State: git.PRStateDraft},
			{Number: 96, Title: "dotfiles pr", HeadBranchID: &dotfilesBranch, State: git.PRStateDraft},
		},
	}

	m, _ := New().Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m, cmd := m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{sess}})
	require.Nil(t, cmd)

	out := m.View()
	assert.Contains(t, out, "utena pr")
	assert.Contains(t, out, "dotfiles pr")
}

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

func TestSessionList_RendersBlockedSection(t *testing.T) {
	m := New()
	m.width, m.height = 60, 20
	m.sessions = []session.Session{
		{Model: gorm.Model{ID: 1}, Name: "live", Status: session.StatusActive},
		{Model: gorm.Model{ID: 2}, Name: "waiting", Status: session.StatusActive, Blockers: []session.SessionBlocker{openBlocker(session.BlockerKindSlack), openBlocker(session.BlockerKindReview)}},
	}
	m.rebuildFiltered()
	out := m.renderList(60)
	assert.Contains(t, out, "Blocked 1")
	assert.NotContains(t, out, "⏸")
	assert.Less(t, strings.Index(out, "live"), strings.Index(out, "Blocked 1"))
	assert.Less(t, strings.Index(out, "Blocked 1"), strings.Index(out, "waiting"))
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

func TestSplitView_ListHasGutterBeforeDivider(t *testing.T) {
	a, b := withClaude(1, "a", claude.StatusReadyForReview), withClaude(2, "b", claude.StatusWorking)
	a.LastUsedAt = time.Now().Add(-3 * time.Hour)
	b.LastUsedAt = time.Now().Add(-20 * time.Minute)
	m := listWith(t, a, b)
	listW := m.listWidth()
	for i, line := range strings.Split(ansi.Strip(m.View()), "\n") {
		cols := []rune(line)
		require.Greater(t, len(cols), listW+2, "line %d", i)
		assert.Equal(t, "  │", string(cols[listW-2:listW+1]), "line %d: %q", i, line)
	}
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if cols := []rune(l); strings.Contains(l, "3h") || strings.Contains(l, "20m") {
			assert.Equal(t, "    │", string(cols[listW-4:listW+1]), "%q", l)
		}
	}
	assert.Equal(t, 120, lipgloss.Width(strings.Split(m.View(), "\n")[0]))
}

func outdatedState() provider.BrowserStateUpdatedMsg {
	return provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Outdated: true, UpdateHint: "reload it in chrome://extensions", Sessions: map[uint]browser.SessionBrowser{}}}
}

func TestSessionList_OutdatedExtensionWarning(t *testing.T) {
	m, _ := New().Update(outdatedState())
	assert.Equal(t, ui.Status{Text: "browser extension outdated — reload it in chrome://extensions", Kind: ui.StatusWarning}, m.Status())

	m, _ = m.Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{}}})
	assert.True(t, m.Status().Empty())
}

func TestSessionList_OutdatedWarningDoesNotClobberActionStatus(t *testing.T) {
	m, _ := New().Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{withClaude(1, "x", claude.StatusIdle)}})
	m, _, _ = pressA(m)
	m, _ = m.Update(outdatedState())
	assert.Contains(t, m.Status().Text, "press a again")

	m, _, _ = m.OnKeyMsg(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	assert.Contains(t, m.Status().Text, "browser extension outdated")
}

func TestSessionList_DriverWarning(t *testing.T) {
	m, _ := New().Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Warning: "allow utena to control Chrome", Sessions: map[uint]browser.SessionBrowser{}}})
	assert.Equal(t, ui.Status{Text: "allow utena to control Chrome", Kind: ui.StatusWarning}, m.Status())

	m, _ = m.Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{}}})
	assert.True(t, m.Status().Empty())
}

func TestSessionList_SplitPreviewShowsBrowserSection(t *testing.T) {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{withClaude(7, "web", claude.StatusIdle)}})
	m, _ = m.Update(provider.BrowserStateUpdatedMsg{State: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{
		7: {State: "open", TabCount: 1, Windows: []browser.WindowView{{ID: "w1", State: "open", Tabs: []browser.TabView{{ID: "t1", Title: "Preview tab", URL: "https://x.example.com", Active: true}}}}},
	}}})
	out := m.View()
	assert.Contains(t, out, "Browser")
	assert.Contains(t, out, "Preview tab")
	assert.NotContains(t, out, "▌ ●")
}

func TestSessionList_InitRequestsBrowserState(t *testing.T) {
	_, cmd := New().Init()
	require.NotNil(t, cmd)
	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)
	var got []string
	for _, c := range batch {
		got = append(got, reflect.TypeOf(c()).String())
	}
	assert.ElementsMatch(t, []string{"provider.fetchSessionsIntentMsg", "provider.requestBrowserStateMsg", "provider.fetchBundlesIntentMsg"}, got)
}
