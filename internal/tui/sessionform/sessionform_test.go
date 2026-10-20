package sessionform

import (
	"errors"
	"fmt"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/bundlepicker"
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
	assert.Equal(t, fieldBundles, m.form.Focused())
	m, _ = m.Update(press("up"))
	assert.Equal(t, fieldWorkspaces, m.form.Focused())
	m, _ = m.Update(press("down"))
	assert.Equal(t, fieldBundles, m.form.Focused())
	m.rowCursor = 2
	m, _ = m.Update(press("down"))
	assert.Equal(t, fieldBranches, m.form.Focused())
	assert.Equal(t, 0, m.rowCursor)
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
		longBundles := []string{long + "x", long + "y", long + "z"}
		m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1, 2, 3, 4}, Bundles: longBundles})
		require.Equal(t, longBundles, m.defaultBundles)
		m, _ = m.focus(fieldBundles)
		out := m.View()
		assert.Contains(t, out, "from workspace defaults")
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

func catalogLoaded(m Model) Model {
	m, _ = m.Update(provider.BundlesLoadedMsg{Bundles: []bundles.Entry{{Name: "go", Plugins: []string{"gopls"}}, {Name: "docs", Plugins: []string{"writer"}}}})
	return m
}

func TestForm_PickingWorkspacesRequestsDefaultBundles(t *testing.T) {
	_, cmd := picked(t, gitWS(1, "api"), gitWS(2, "web"))
	assert.Contains(t, uitest.MsgTypes(cmd), "provider.fetchDefaultBundlesIntentMsg")
}

func TestForm_InitFetchesBundleCatalog(t *testing.T) {
	m := New()
	_, cmd := m.Init()
	assert.Contains(t, uitest.MsgTypes(cmd), "provider.fetchBundlesIntentMsg")
}

func TestForm_BundlesShowWorkspaceDefaultsUntilLoaded(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	assert.Contains(t, m.View(), "workspace defaults")
	m, cmd := m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Err: errors.New("x")})
	assert.Nil(t, cmd)
	assert.Contains(t, m.View(), "workspace defaults")
	assert.True(t, m.Status().Empty())
	assert.False(t, m.submitting)
	assert.Nil(t, m.defaultBundles)
}

func TestForm_DefaultBundlesPreCheck(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	assert.Equal(t, []string{"go"}, m.defaultBundles)
	assert.Nil(t, m.selectedBundles)
	m, _ = m.focus(fieldBundles)
	assert.Contains(t, m.View(), "from workspace defaults")
}

func TestForm_LoadedEmptyDefaultsShowNone(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{}})
	assert.Contains(t, m.View(), "none")
	assert.NotContains(t, m.View(), "workspace defaults")
}

func TestForm_LoadedEmptyDefaultsWithoutCatalog(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{}})
	assert.Contains(t, m.View(), "no bundles configured")
}

func TestForm_StaleDefaultBundlesIgnored(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1, 2}, Bundles: []string{"go"}})
	assert.Nil(t, m.selectedBundles)
	assert.Nil(t, m.defaultBundles)
}

func TestForm_BundlesFieldOpensPicker(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m, _ = m.focus(fieldBundles)
	assert.False(t, m.CapturesText())
	m, _ = m.Update(press("enter"))
	assert.Equal(t, overlayBundles, m.overlay)
	v := m.View()
	assert.Contains(t, v, "Bundles")
	assert.Contains(t, v, "go")
	assert.Contains(t, v, "docs")
}

func TestForm_EditedBundlesStopRecompute(t *testing.T) {
	api, web := gitWS(1, "api"), gitWS(2, "web")
	m, _ := picked(t, api, web)
	m = catalogLoaded(m)
	m, _ = m.focus(fieldBundles)
	m, _ = m.Update(press("enter"))
	require.Equal(t, overlayBundles, m.overlay)
	m, _ = m.Update(bundlepicker.SelectedMsg{Bundles: []string{"docs"}})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldBundles, m.form.Focused())
	assert.Equal(t, []string{"docs"}, m.selectedBundles)
	assert.NotContains(t, m.View(), "from workspace defaults")

	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1, 2}, Bundles: []string{"go"}})
	assert.Equal(t, []string{"docs"}, m.selectedBundles)

	m = m.openWorkspacePicker()
	m, cmd := m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{api}})
	assert.NotContains(t, uitest.MsgTypes(cmd), "provider.fetchDefaultBundlesIntentMsg")
	assert.Equal(t, []string{"docs"}, m.selectedBundles)
}

func TestForm_SubmitOmitsBundlesUntilEdited(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = loaded(m, 1, "main", "main")
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	assert.Nil(t, m.selectedBundles)
	_, cmd := m.Update(press("enter"))
	assert.Equal(t, []string{"provider.createSessionIntentMsg"}, uitest.MsgTypes(cmd))

	m.overlay = overlayBundles
	m, _ = m.Update(bundlepicker.SelectedMsg{Bundles: []string{}})
	got := m.selectedBundles
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func openBundlePicker(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = m.focus(fieldBundles)
	m, _ = m.Update(press("enter"))
	require.Equal(t, overlayBundles, m.overlay)
	return m
}

func TestForm_LateCatalogRefreshesOpenPicker(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	m = openBundlePicker(t, m)
	assert.Contains(t, m.View(), "unknown bundle")
	m = catalogLoaded(m)
	v := m.View()
	assert.Contains(t, v, "[x] go")
	assert.Contains(t, v, "docs")
	assert.NotContains(t, v, "unknown bundle")
}

func TestForm_ConfirmEmptyBeforeDefaultsLoadedIsNoChange(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m = openBundlePicker(t, m)
	m, _ = m.Update(bundlepicker.SelectedMsg{Bundles: []string{}})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldBundles, m.form.Focused())
	assert.Nil(t, m.selectedBundles)

	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	assert.Equal(t, []string{"go"}, m.defaultBundles)
}

func TestForm_ConfirmUnchangedDefaultsIsNotEdited(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	m = openBundlePicker(t, m)
	m, _ = m.Update(bundlepicker.SelectedMsg{Bundles: []string{"go"}})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Nil(t, m.selectedBundles)
	assert.Contains(t, m.View(), "from workspace defaults")
}

func TestForm_DefaultsArrivingRefreshOpenPicker(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m = openBundlePicker(t, m)
	assert.NotContains(t, m.View(), "[x] go")
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	assert.Contains(t, m.View(), "[x] go")
}

func TestForm_ConfirmEmptyAfterDefaultsMarksEdited(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m, _ = m.Update(provider.DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1}, Bundles: []string{"go"}})
	m = openBundlePicker(t, m)
	m, _ = m.Update(bundlepicker.SelectedMsg{Bundles: []string{}})
	got := m.selectedBundles
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestForm_EscFromBundlePickerReturnsToField(t *testing.T) {
	m, _ := picked(t, gitWS(1, "api"))
	m = catalogLoaded(m)
	m, _ = m.focus(fieldBundles)
	m, _ = m.Update(press("enter"))
	require.Equal(t, overlayBundles, m.overlay)
	m, _ = closeOverlay(m)
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldBundles, m.form.Focused())
	assert.Nil(t, m.selectedBundles)
}

func submittedBackend(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	require.NotNil(t, cmd)
	msg := cmd()
	assert.Equal(t, "provider.createSessionIntentMsg", fmt.Sprintf("%T", msg))
	return reflect.ValueOf(msg).FieldByName("backend").String()
}

func namedAndLoaded(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{gitWS(1, "api")}})
	m = loaded(m, 1, "main", "main")
	for _, r := range "my-feature" {
		m, _ = m.Update(press(string(r)))
	}
	return m
}

func TestForm_InitFetchesCoderStatus(t *testing.T) {
	_, cmd := New().Init()
	assert.Contains(t, uitest.MsgTypes(cmd), "provider.fetchCoderStatusIntentMsg")
}

func TestForm_BackendHiddenWithoutCoder(t *testing.T) {
	m := namedAndLoaded(t, started(t, gitWS(1, "api")))
	m, _ = m.Update(provider.CoderStatusLoadedMsg{Configured: false})
	assert.NotContains(t, m.View(), "Backend")
	m, _ = m.Update(press("tab"))
	assert.Equal(t, fieldName, m.form.Focused(), "name stays the last field")
	_, cmd := m.Update(press("enter"))
	assert.Equal(t, "", submittedBackend(t, cmd))
}

func TestForm_BackendChoiceWhenCoderConfigured(t *testing.T) {
	m := started(t, gitWS(1, "api"))
	m, _ = m.Update(provider.CoderStatusLoadedMsg{Configured: true})
	m = namedAndLoaded(t, m)
	assert.Contains(t, m.View(), "Backend")
	assert.Contains(t, m.View(), "● local")

	m, _ = m.Update(press("tab"))
	assert.Equal(t, fieldBackend, m.form.Focused())
	assert.True(t, m.Keys().(keyMap).Create.Enabled())
	m, _ = m.Update(press("right"))
	assert.Equal(t, "coder", m.backend.Value())
	assert.Contains(t, m.View(), "● coder")
	_, cmd := m.Update(press("enter"))
	assert.Equal(t, "coder", submittedBackend(t, cmd))
}

func TestForm_LocalBackendSendsNoBackend(t *testing.T) {
	m := started(t, gitWS(1, "api"))
	m, _ = m.Update(provider.CoderStatusLoadedMsg{Configured: true})
	m = namedAndLoaded(t, m)
	_, cmd := m.Update(press("enter"))
	assert.Equal(t, "", submittedBackend(t, cmd))
}

func TestForm_CoderStatusErrorHidesBackend(t *testing.T) {
	m := started(t, gitWS(1, "api"))
	m, _ = m.Update(provider.CoderStatusLoadedMsg{Configured: true})
	m, _ = m.Update(provider.CoderStatusLoadedMsg{Err: errors.New("down")})
	assert.False(t, m.coderAvailable)
	assert.NotContains(t, m.View(), "Backend")
}
