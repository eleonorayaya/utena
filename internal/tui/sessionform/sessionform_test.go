package sessionform

import (
	"errors"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitWS(id uint, name string) workspace.Workspace {
	ws := workspace.Workspace{Name: name, Path: "/tmp/" + name, IsGitRepo: true}
	ws.ID = id
	return ws
}

func press(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func closeOverlay(m Model) (Model, tea.Cmd) {
	m, cmd := m.Update(press("esc"))
	if cmd != nil {
		return m.Update(cmd())
	}
	return m, nil
}

func started(t *testing.T, wss ...workspace.Workspace) Model {
	t.Helper()
	m := New()
	m.SetSize(100, 30)
	m, _ = m.Init()
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: wss})
	return m
}

func picked(t *testing.T, wss ...workspace.Workspace) (Model, tea.Cmd) {
	t.Helper()
	m := started(t, wss...)
	return m.Update(workspacepicker.SelectedMsg{Workspaces: wss})
}

func loaded(m Model, id uint, def string, branches ...string) Model {
	m, _ = m.Update(provider.BranchesLoadedMsg{WorkspaceID: id, Branches: branches, DefaultBranch: def})
	return m
}

func TestForm_OpensIntoWorkspacePickerAndReturnsToName(t *testing.T) {
	m := started(t, gitWS(1, "api"), gitWS(2, "web"))
	assert.Equal(t, overlayWorkspaces, m.overlay)
	assert.Contains(t, m.View(), "Workspaces")

	m, cmd := m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{gitWS(1, "api"), gitWS(2, "web")}})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldName, m.form.Focused())
	assert.Equal(t, 2, strings.Count(strings.Join(uitest.MsgTypes(cmd), " "), "provider.requestBranchesMsg"))
	assert.True(t, m.CapturesText())
}

func TestForm_RowsDefaultToResolvedBranchAndShowLoading(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"), gitWS(2, "web"))
	assert.Contains(t, m.View(), "…")
	m = loaded(m, 1, "develop", "main", "develop")
	m = loaded(m, 2, "", "trunk")
	assert.Equal(t, "develop", m.rows[0].Branch)
	assert.Equal(t, "trunk", m.rows[1].Branch)
	assert.NotContains(t, m.View(), "…")
}

func TestForm_ChangingWorkspacesKeepsExistingPicks(t *testing.T) {
	api, web, data := gitWS(1, "api"), gitWS(2, "web"), gitWS(3, "data")
	m, _ := picked(t, api, web)
	m = loaded(m, 1, "main", "main", "feature")
	m.rows[0] = m.rows[0].Pick("feature")

	m = m.openWorkspacePicker()
	assert.Contains(t, m.View(), "[x] api")
	m, cmd := m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{api, data}})
	require.Len(t, m.rows, 2)
	assert.Equal(t, "feature", m.rows[0].Branch)
	assert.Equal(t, uint(3), m.rows[1].Workspace.ID)
	assert.Equal(t, 1, strings.Count(strings.Join(uitest.MsgTypes(cmd), " "), "provider.requestBranchesMsg"))
}

func TestForm_SubmitBlockedWhileBranchesLoad(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, cmd := m.Update(press("enter"))
	assert.Nil(t, cmd)
	assert.Equal(t, "waiting for branch info", m.Status().Text)
}

func TestForm_EnterOnNameCreatesSession(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	for _, r := range "my-feature" {
		m, _ = m.Update(press(string(r)))
	}
	m, cmd := m.Update(press("enter"))
	assert.Equal(t, []string{"provider.createSessionIntentMsg"}, uitest.MsgTypes(cmd))
	assert.True(t, m.submitting)
	_, cmd = m.Update(press("enter"))
	assert.Nil(t, cmd, "repeated submits are ignored")
	assert.Equal(t, []provider.SessionWorkspaceSpec{{WorkspaceID: 1, BaseBranch: "main"}}, buildSpecs(m.rows, true))
}

func TestForm_UseExistingModeSendsBranch(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	m, _ = m.Update(press("shift+tab"))
	assert.Equal(t, fieldMode, m.form.Focused())
	m, _ = m.Update(press("right"))
	assert.Equal(t, "use existing", m.mode.Value())
	assert.Equal(t, []provider.SessionWorkspaceSpec{{WorkspaceID: 1, Branch: "main"}}, buildSpecs(m.rows, false))
}

func TestForm_InvalidNameErrorShowsUnderName(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	for _, r := range "bad name!" {
		m, _ = m.Update(press(string(r)))
	}
	m, cmd := m.Update(press("enter"))
	assert.Nil(t, cmd)
	assert.NotEmpty(t, m.nameErr)
	lines := strings.Split(m.View(), "\n")
	nameLine := -1
	for i, l := range lines {
		if strings.Contains(l, "Name") {
			nameLine = i
		}
	}
	require.GreaterOrEqual(t, nameLine, 0)
	assert.Contains(t, lines[nameLine+1], "contains invalid characters")
}

func TestForm_EscFromPickerReturnsToPageAndEscOnPageGoesBack(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(press("shift+tab"))
	m, _ = m.Update(press("shift+tab"))
	assert.Equal(t, fieldBranches, m.form.Focused())
	m, _ = m.Update(press("enter"))
	assert.Equal(t, overlayBranch, m.overlay)
	m, cmd := closeOverlay(m)
	assert.Equal(t, overlayNone, m.overlay)
	assert.Nil(t, cmd)

	_, cmd = m.Update(press("esc"))
	require.NotNil(t, cmd)
	assert.IsType(t, router.BackMsg{}, cmd())
}

func TestForm_BranchPickerSelectionUpdatesRow(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"), gitWS(2, "web"))
	m = loaded(m, 1, "main", "main", "feature")
	m = loaded(m, 2, "main", "main")
	m, _ = m.Update(press("shift+tab"))
	m, _ = m.Update(press("shift+tab"))
	m, _ = m.Update(press("enter"))
	require.Equal(t, overlayBranch, m.overlay)
	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "feature"})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, "feature", m.rows[0].Branch)
	assert.False(t, m.rows[0].IsDefault())
}

func TestForm_BranchRowsNavigateBeforeLeavingField(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"), gitWS(2, "web"), gitWS(3, "data"))
	m, _ = m.Update(press("up"))
	m, _ = m.Update(press("up"))
	assert.Equal(t, fieldBranches, m.form.Focused())
	assert.Equal(t, 2, m.rowCursor)
	m, _ = m.Update(press("up"))
	m, _ = m.Update(press("up"))
	assert.Equal(t, 0, m.rowCursor)
	assert.Equal(t, fieldBranches, m.form.Focused())
	m, _ = m.Update(press("up"))
	assert.Equal(t, fieldWorkspaces, m.form.Focused())
}

func TestForm_ManualBranchNotFoundShowsUnderRow(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	m.pickingRow = 0
	m.overlay = overlayBranch
	m, _ = m.Update(branchpicker.ManualEntryRequestedMsg{})
	require.True(t, m.rows[0].Editing)
	assert.True(t, m.CapturesText())
	for _, r := range "nope" {
		m, _ = m.Update(press(string(r)))
	}
	m, cmd := m.Update(press("enter"))
	assert.Equal(t, []string{"provider.checkBranchExistsIntentMsg"}, uitest.MsgTypes(cmd))
	m, _ = m.Update(provider.BranchExistsCheckedMsg{WorkspaceID: 1, Name: "nope"})
	assert.Contains(t, m.View(), "✕ branch not found: nope")
	assert.True(t, m.rows[0].Editing)
}

func TestForm_BranchFetchErrorShowsUnderRow(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(provider.BranchesLoadedMsg{WorkspaceID: 1, Err: errors.New("boom")})
	assert.Contains(t, m.View(), "✕ boom")
	assert.Equal(t, "main", m.rows[0].Branch)
}

func TestForm_NonGitWorkspaceKeepsPickerOpen(t *testing.T) {
	m := started(t)
	plain := workspace.Workspace{Name: "notes"}
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{plain}})
	assert.Equal(t, overlayWorkspaces, m.overlay)
	assert.Contains(t, m.Status().Text, "not a git repository")
}

func TestForm_LinesFitWidthWithManyLongWorkspaces(t *testing.T) {
	long := strings.Repeat("very-long-workspace-name-", 4)
	wss := []workspace.Workspace{gitWS(1, long+"a"), gitWS(2, long+"b"), gitWS(3, "c"), gitWS(4, "d")}
	for _, w := range []int{40, 80, 200} {
		m := New()
		m.SetSize(w, 24)
		m, _ = m.Init()
		m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: wss})
		for _, v := range []string{m.View()} {
			for _, l := range strings.Split(v, "\n") {
				assert.LessOrEqual(t, lipgloss.Width(l), w, "picker %d: %q", w, l)
			}
		}
		m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: wss})
		for i, ws := range wss {
			m = loaded(m, ws.ID, strings.Repeat("feature/branch-", i+3))
		}
		out := m.View()
		assert.Equal(t, 24, len(strings.Split(out, "\n")))
		for _, l := range strings.Split(out, "\n") {
			assert.LessOrEqual(t, lipgloss.Width(l), w, "page %d: %q", w, l)
		}
	}
}

func TestForm_ReInitResetsState(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Init()
	assert.Nil(t, m.rows)
	assert.Equal(t, overlayWorkspaces, m.overlay)
}

func TestForm_EscFromInitialPickerFocusesWorkspaces(t *testing.T) {
	m := started(t, gitWS(1, "api"))
	m, _ = closeOverlay(m)
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldWorkspaces, m.form.Focused())
	assert.False(t, m.CapturesText())
}

func TestForm_FetchedRemoteBranchesSurviveReopen(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	m, _ = m.Update(provider.BranchesFetchedMsg{WorkspaceID: 1, Branches: []provider.BranchRef{{Name: "main"}, {Name: "remote-only", Remote: true}}})
	assert.Equal(t, []string{"remote-only"}, m.rows[0].Remote)
	m = m.openBranchPicker(0)
	assert.Contains(t, m.View(), "remote-only")
}
