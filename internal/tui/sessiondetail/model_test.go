package sessiondetail

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
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
	assert.Contains(t, out, "#116 utena pr")
	assert.Contains(t, out, "#96 dotfiles pr")
}

func TestSessionDetail_RefreshUpdatesEmbeddedPRs(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeMultiWorkspaceSession(1, 2)})

	refreshed := makeMultiWorkspaceSession(1, 2)
	refreshed.PullRequests[0].State = git.PRStateMerged
	m, _ = m.Update(provider.SessionsStateUpdatedMsg{Sessions: []session.Session{refreshed}})

	assert.Contains(t, m.View(), "#116 utena pr [merged]")
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
		StatusError: "tmux setup failed",
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
	assert.Contains(t, m.View(), "[!]")
	assert.Contains(t, m.View(), "branch not pulled")
}

func TestSessionDetail_View_NoWarningWhenClean(t *testing.T) {
	m, _ := New().Update(SelectMsg{Session: makeActiveSession()})
	assert.NotContains(t, m.View(), "[!]")
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
	assert.Contains(t, out, "Blockers")
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

	assert.NotContains(t, m.View(), "Blockers")
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
	assert.NotContains(t, out, "›")
}

func TestRenderPanel_ShowsPastBranchesAndTheirPRs(t *testing.T) {
	current, past := uint(2), uint(1)
	s := session.Session{
		Model:  gorm.Model{ID: 9},
		Name:   "stacked",
		Status: session.StatusActive,
		Worktrees: []session.SessionWorktree{{
			Worktree: &git.Worktree{BranchID: current, Branch: &git.Branch{Model: gorm.Model{ID: current}, Name: "eqt/top"}},
		}},
		Branches: []session.SessionBranch{
			{BranchID: past, Branch: &git.Branch{Model: gorm.Model{ID: past}, Name: "eqt/base"}},
			{BranchID: current, Branch: &git.Branch{Model: gorm.Model{ID: current}, Name: "eqt/top"}},
		},
		PullRequests: []git.PullRequest{
			{Number: 1, Title: "base", HeadBranchID: &past, State: git.PRStateOpen},
			{Number: 2, Title: "top", HeadBranchID: &current, State: git.PRStateDraft},
		},
	}

	out := RenderPanel(&s, 80, 0)

	assert.Contains(t, out, "eqt/top")
	assert.Contains(t, out, "Past")
	assert.Contains(t, out, "eqt/base")
	assert.Contains(t, out, "#1 base")
	assert.Contains(t, out, "#2 top")
}
