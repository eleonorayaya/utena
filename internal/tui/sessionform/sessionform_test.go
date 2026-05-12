package sessionform

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func TestForm_PickThenCreate_RetainsWorkspaceIDs(t *testing.T) {
	m := New()
	m.SetSize(80, 24)

	m, _ = m.Init()

	ws := workspace.Workspace{Name: "test", Path: "/tmp/test", IsGitRepo: true}
	ws.ID = 42
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{
		Workspaces: []workspace.Workspace{ws},
	})

	m, _ = m.Update(workspacepicker.SelectedMsg{
		Workspace:  ws,
		Workspaces: []workspace.Workspace{ws},
	})
	require.Equal(t, []workspace.Workspace{ws}, m.selectedWorkspaces, "form should retain picked workspaces after SelectedMsg")
	require.Equal(t, branchPickerStep, m.activeStep)

	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "main"})
	require.Equal(t, "main", m.selectedBranch)
	require.Equal(t, branchModeStep, m.activeStep)
	require.Equal(t, []workspace.Workspace{ws}, m.selectedWorkspaces, "selectedWorkspaces must survive branch pick")

	ids := workspaceIDs(m.selectedWorkspaces)
	require.Equal(t, []uint{42}, ids, "workspaceIDs() must yield [42]")
}

// Toggle-then-select multi pick must surface both workspaces to selectedWorkspaces.
func TestForm_MultiSelectFromPicker_RetainsAllIDs(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m, _ = m.Init()

	ws1 := workspace.Workspace{Name: "a", Path: "/tmp/a", IsGitRepo: true}
	ws1.ID = 10
	ws2 := workspace.Workspace{Name: "b", Path: "/tmp/b", IsGitRepo: true}
	ws2.ID = 20
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: []workspace.Workspace{ws1, ws2}})

	m, _ = m.Update(workspacepicker.SelectedMsg{
		Workspace:  ws1,
		Workspaces: []workspace.Workspace{ws1, ws2},
	})
	require.Equal(t, []uint{10, 20}, workspaceIDs(m.selectedWorkspaces), "multi-select must yield both ids")
}

// Re-entering the form (e.g. after a previous session creation completed and the
// user navigated back) should NOT carry state from the previous run AND should
// still set workspace_ids correctly on the next create.
func TestForm_ReInitAfterPreviousCreate_PicksWorkspaceCleanly(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m, _ = m.Init()

	wsA := workspace.Workspace{Name: "first", Path: "/tmp/a", IsGitRepo: true}
	wsA.ID = 1
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: []workspace.Workspace{wsA}})
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspace: wsA, Workspaces: []workspace.Workspace{wsA}})
	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "main"})
	require.Equal(t, []uint{1}, workspaceIDs(m.selectedWorkspaces))

	// Simulate: user finished, navigated away to progress, then back -> Init runs
	m, _ = m.Init()
	require.Nil(t, m.selectedWorkspaces, "Init must reset selectedWorkspaces")
	require.Equal(t, workspacePickerStep, m.activeStep)

	wsB := workspace.Workspace{Name: "second", Path: "/tmp/b", IsGitRepo: true}
	wsB.ID = 2
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: []workspace.Workspace{wsB}})
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspace: wsB, Workspaces: []workspace.Workspace{wsB}})
	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "develop"})
	require.Equal(t, []uint{2}, workspaceIDs(m.selectedWorkspaces), "second create must carry the freshly-picked workspace id")
}
