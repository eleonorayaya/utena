package sessiondetail

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/bundlepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func makeMultiWorkspaceSession(utenaBranch, dotfilesBranch uint) session.Session {
	s := makeActiveSession()
	s.Worktrees = []session.SessionWorktree{
		{
			Worktree:  &git.Worktree{BranchID: utenaBranch, Branch: &git.Branch{Model: gorm.Model{ID: utenaBranch}, Name: "eqt/x"}},
			Workspace: &workspace.Workspace{Model: gorm.Model{ID: 10}, Name: "utena"},
		},
		{
			Worktree:  &git.Worktree{BranchID: dotfilesBranch, Branch: &git.Branch{Model: gorm.Model{ID: dotfilesBranch}, Name: "eqt/x"}},
			Workspace: &workspace.Workspace{Model: gorm.Model{ID: 20}, Name: "dotfiles"},
		},
	}
	s.PullRequests = []git.PullRequest{
		{Number: 116, Title: "utena pr", HeadBranchID: &utenaBranch, State: git.PRStateDraft},
		{Number: 96, Title: "dotfiles pr", HeadBranchID: &dotfilesBranch, State: git.PRStateDraft},
	}
	return s
}

func TestSessionDetail_ShowsEmbeddedPRsFromEveryWorkspace(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeMultiWorkspaceSession(1, 2)})

	out := m.View()
	assert.Contains(t, out, "utena pr")
	assert.Contains(t, out, "dotfiles pr")
	assert.Contains(t, out, "#116")
	assert.Contains(t, out, "#96")
}

func TestSessionDetail_RefreshUpdatesEmbeddedPRs(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeMultiWorkspaceSession(1, 2)})

	refreshed := makeMultiWorkspaceSession(1, 2)
	refreshed.PullRequests[0].State = git.PRStateMerged
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{refreshed}})

	assert.Contains(t, m.View(), "⊕ merged")
}

func makeActiveSession() session.Session {
	return session.Session{
		Model:  gorm.Model{ID: 1},
		Name:   "my-feature",
		Status: session.StatusActive,
	}
}

func makeBrokenSession() session.Session {
	return session.Session{
		Model:       gorm.Model{ID: 2},
		Name:        "broken",
		Status:      session.StatusBroken,
		StatusError: "mux setup failed",
	}
}

func makeArchivedSession() session.Session {
	return session.Session{
		Model:  gorm.Model{ID: 3},
		Name:   "archived",
		Status: session.StatusArchived,
	}
}

func TestSessionDetail_SelectMsg_LoadsSession(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	require.NotNil(t, m.sess)
	assert.Equal(t, "my-feature", m.sess.Name)
}

func TestSessionDetail_Back_NavigatesBack(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	msg := cmd()
	assert.IsType(t, router.BackMsg{}, msg)
}

func TestSessionDetail_Activate_SkipsWhenAttached(t *testing.T) {
	attached := makeActiveSession()
	attached.IsAttached = true
	m, _ := New().Update(SelectMsg{Session: attached})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd)
}

func TestSessionDetail_Archive_ArchivesAndGoesBack(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.NotNil(t, cmd)
}

func TestSessionDetail_Archive_ArchivesWhenBroken(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBrokenSession()})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.NotNil(t, cmd)
}

func TestSessionDetail_Delete_SkipsWhenActive(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.Nil(t, cmd)
	assert.Equal(t, uint(0), m2.pendingDeleteID)
}

func TestSessionDetail_Delete_RequiresDoublePress(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeArchivedSession()})
	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.Nil(t, cmd)
	assert.Equal(t, uint(3), m2.pendingDeleteID)

	_, cmd2 := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.NotNil(t, cmd2)
}

func TestSessionDetail_Delete_ResetsByInterveningKey(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeArchivedSession()})
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	assert.Equal(t, uint(3), m2.pendingDeleteID)

	m3, _ := m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, uint(0), m3.pendingDeleteID)
}

func TestSessionDetail_Repair_SkipsWhenHealthy(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.Nil(t, cmd)
}

func TestSessionDetail_Repair_FiresWhenBroken(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBrokenSession()})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.NotNil(t, cmd)
}

func TestSessionDetail_View_ShowsWarning(t *testing.T) {
	warned := session.Session{
		Model:       gorm.Model{ID: 3},
		Name:        "warn",
		Status:      session.StatusActive,
		StatusError: "branch not pulled: dirty",
	}
	m, _ := New().Update(SelectMsg{Session: warned})
	assert.Contains(t, m.View(), "⚠")
	assert.Contains(t, m.View(), "branch not pulled")
}

func TestSessionDetail_View_NoWarningWhenClean(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	assert.NotContains(t, m.View(), "⚠")
}

func makeBlockedSession() session.Session {
	s := makeActiveSession()
	s.Blockers = []session.SessionBlocker{
		{Model: gorm.Model{ID: 10}, Kind: session.BlockerKindReview, Note: "first"},
		{Model: gorm.Model{ID: 11}, Kind: session.BlockerKindSlack, Note: "second"},
	}
	return s
}

func runeKey(r string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)}
}

func TestSessionDetail_RendersOpenBlockers(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	out := m.View()
	assert.Contains(t, out, "Blocked 2")
	assert.Contains(t, out, "first")
	assert.Contains(t, out, "second")
}

func TestSessionDetail_ResolveHighlightedBlocker(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("j"))
	_, cmd := m.Update(runeKey("u"))
	require.NotNil(t, cmd)
	msg := cmd()
	assert.Equal(t, "provider.resolveBlockerIntentMsg", reflect.TypeOf(msg).String())
	assert.Contains(t, fmt.Sprintf("%+v", msg), "blockerID:11")
}

func TestSessionDetail_DeleteBlockerNeedsTwoPresses(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, cmd := m.Update(runeKey("x"))
	assert.Nil(t, cmd)
	_, cmd = m.Update(runeKey("x"))
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.deleteBlockerIntentMsg", reflect.TypeOf(cmd()).String())
}

func TestSessionDetail_RefreshClampsCursorWhenLastBlockerResolved(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeBlockedSession()})
	m, _ = m.Update(runeKey("j"))

	now := time.Now()
	refreshed := makeBlockedSession()
	refreshed.Blockers[0].ResolvedAt = &now
	refreshed.Blockers[1].ResolvedAt = &now
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{refreshed}})

	assert.NotContains(t, m.View(), "Blocked")
	_, cmd := m.Update(runeKey("u"))
	assert.Nil(t, cmd)
	_, cmd = m.Update(runeKey("x"))
	assert.Nil(t, cmd)
}

func TestSessionDetail_BlockKeyOpensForm(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	_, cmd := m.Update(runeKey("b"))
	require.NotNil(t, cmd)
}

func TestRenderPanel_ShowsBlockersWithoutCursor(t *testing.T) {
	s := makeBlockedSession()
	out := RenderPanel(&s, 60, 30)
	assert.Contains(t, out, "first")
	assert.NotContains(t, out, "▌")
}

func TestRenderPanel_PastBranchPRsStillShowUnderTheirRepo(t *testing.T) {
	current, past := uint(2), uint(1)
	s := session.Session{
		Model:     gorm.Model{ID: 9},
		Name:      "stacked",
		Status:    session.StatusActive,
		Worktrees: []session.SessionWorktree{wt(0, "utena", 5, current)},
		PullRequests: []git.PullRequest{
			{Number: 1, Title: "base", RepoID: 5, HeadBranchID: &past, State: git.PRStateOpen},
			{Number: 2, Title: "top", RepoID: 5, HeadBranchID: &current, State: git.PRStateDraft},
		},
	}
	out := RenderPanel(&s, 80, 0)
	assert.Contains(t, out, "base")
	assert.Contains(t, out, "top")
	assert.NotContains(t, out, "eqt/")
	assert.NotContains(t, out, "Other")
}

func bundleCatalog() []bundles.Entry {
	return []bundles.Entry{
		{Name: "go", Plugins: []string{"go@m"}},
		{Name: "docs", Plugins: []string{"pdf@m"}},
	}
}

func feedAll(m Model, msg tea.Msg) Model {
	m, cmd := m.Update(msg)
	for _, out := range uitest.Drain(cmd) {
		if fm, ok := out.(list.FilterMatchesMsg); ok {
			m, _ = m.Update(fm)
		}
	}
	return m
}

func bundleDetail(s session.Session) Model {
	m, _ := New().Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(SelectMsg{Session: s})
	m, _ = m.Update(provider.BundlesLoadedMsg{Bundles: bundleCatalog()})
	return m
}

func TestSessionDetail_SelectFetchesBundles(t *testing.T) {
	_, cmd := New().Update(SelectMsg{Session: makeActiveSession()})
	assert.Contains(t, uitest.MsgTypes(cmd), "provider.fetchBundlesIntentMsg")
}

func TestSessionDetail_SelectSkipsFetchOnceCatalogLoaded(t *testing.T) {
	m := bundleDetail(makeActiveSession())
	_, cmd := m.Update(SelectMsg{Session: makeActiveSession()})
	assert.NotContains(t, uitest.MsgTypes(cmd), "provider.fetchBundlesIntentMsg")
}

func TestSessionDetail_FailedCatalogLoadKeepsFetching(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	m, _ = m.Update(provider.BundlesLoadedMsg{Err: errors.New("down")})
	_, cmd := m.Update(SelectMsg{Session: makeActiveSession()})
	assert.Contains(t, uitest.MsgTypes(cmd), "provider.fetchBundlesIntentMsg")
}

func TestSessionDetail_PluginsKeyOpensPickerAndSaves(t *testing.T) {
	s := makeActiveSession()
	s.Bundles = []string{"go"}
	m := bundleDetail(s)
	m, _ = m.Update(runeKey("p"))
	require.True(t, m.pickingBundles)
	assert.Contains(t, m.View(), "[x] go")

	m, cmd := m.Update(bundlepicker.SelectedMsg{Bundles: []string{"docs"}})
	assert.Equal(t, []string{"provider.setSessionBundlesIntentMsg"}, uitest.MsgTypes(cmd))
	assert.False(t, m.pickingBundles)
	out := m.View()
	assert.Contains(t, out, "my-feature")
	assert.NotContains(t, out, "[x]")
}

func TestSessionDetail_PluginsKeyIgnoredWhenDeleted(t *testing.T) {
	s := makeActiveSession()
	s.Status = session.StatusDeleted
	m := bundleDetail(s)
	m, _ = m.Update(runeKey("p"))
	assert.False(t, m.pickingBundles)
}

func TestSessionDetail_EscClosesBundlePicker(t *testing.T) {
	m := bundleDetail(makeActiveSession())
	m, _ = m.Update(runeKey("p"))
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	msgs := uitest.Drain(cmd)
	require.Len(t, msgs, 1)
	assert.IsType(t, bundlepicker.ClosedMsg{}, msgs[0])
	assert.True(t, m.pickingBundles)

	m, _ = m.Update(msgs[0])
	assert.False(t, m.pickingBundles)
}

func TestSessionDetail_PickerKeysDontTriggerActions(t *testing.T) {
	s := makeArchivedSession()
	m := bundleDetail(s)
	m, _ = m.Update(runeKey("p"))
	m, _ = m.Update(runeKey("d"))
	assert.True(t, m.pickingBundles)
	assert.Equal(t, uint(0), m.pendingDeleteID)
	assert.True(t, m.Status().Empty())
}

func TestSessionDetail_BundlePickerFilters(t *testing.T) {
	m := bundleDetail(makeActiveSession())
	m = feedAll(m, runeKey("p"))
	m = feedAll(m, runeKey("/"))
	m = feedAll(m, runeKey("g"))
	m = feedAll(m, runeKey("o"))
	out := m.View()
	assert.Contains(t, out, "go")
	assert.NotContains(t, out, "docs")
}

func TestSessionDetail_BundlesSavedShowsReloadHint(t *testing.T) {
	m := bundleDetail(makeActiveSession())
	m, _ = m.Update(provider.SessionBundlesSetMsg{SessionID: 99})
	assert.True(t, m.Status().Empty())
	m, _ = m.Update(provider.SessionBundlesSetMsg{SessionID: 1})
	assert.Contains(t, m.Status().Text, "/reload-plugins")
	assert.Contains(t, m.Status().Text, "/mcp")
}

func TestSessionDetail_CapturesTextWhileFiltering(t *testing.T) {
	m := bundleDetail(makeActiveSession())
	assert.False(t, m.CapturesText())
	m = feedAll(m, runeKey("p"))
	assert.False(t, m.CapturesText())
	m = feedAll(m, runeKey("/"))
	assert.True(t, m.CapturesText())
}

func TestSessionDetail_LateCatalogRefreshesOpenPicker(t *testing.T) {
	s := makeActiveSession()
	s.Bundles = []string{"go"}
	m, _ := New().Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = m.Update(SelectMsg{Session: s})
	m, _ = m.Update(runeKey("p"))
	require.True(t, m.pickingBundles)
	assert.Contains(t, m.View(), "unknown bundle")
	m, _ = m.Update(provider.BundlesLoadedMsg{Bundles: bundleCatalog()})
	out := m.View()
	assert.Contains(t, out, "[x] go")
	assert.Contains(t, out, "docs")
	assert.NotContains(t, out, "unknown bundle")
}

func TestSessionDetail_BundlePickerClosesWhenSessionGone(t *testing.T) {
	open := func() Model {
		m := bundleDetail(makeActiveSession())
		m, _ = m.Update(runeKey("p"))
		require.True(t, m.pickingBundles)
		return m
	}

	m, _ := open().Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{makeArchivedSession()}})
	assert.False(t, m.pickingBundles)

	deleted := makeActiveSession()
	deleted.Status = session.StatusDeleted
	m, _ = open().Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{deleted}})
	assert.False(t, m.pickingBundles)

	m, _ = open().Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{makeActiveSession()}})
	assert.True(t, m.pickingBundles)
}

func TestSessionDetail_UnknownBundleMarked(t *testing.T) {
	s := makeActiveSession()
	s.Bundles = []string{"go", "legacy"}
	m, _ := New().Update(SelectMsg{Session: s})
	m, _ = m.Update(provider.BundlesLoadedMsg{Bundles: []bundles.Entry{{Name: "go"}}})
	assert.Contains(t, m.View(), "legacy (unknown)")
}
