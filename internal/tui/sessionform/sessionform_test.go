package sessionform

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

func TestForm_SingleWorkspaceFlow_RetainsSelectionAndBranch(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m, _ = m.Init()

	ws := workspace.Workspace{Name: "test", Path: "/tmp/test", IsGitRepo: true}
	ws.ID = 42
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: []workspace.Workspace{ws}})

	m, _ = m.Update(workspacepicker.SelectedMsg{
		Workspace:  ws,
		Workspaces: []workspace.Workspace{ws},
	})
	require.Equal(t, []workspace.Workspace{ws}, m.selectedWorkspaces)
	require.Equal(t, branchPickerStep, m.activeStep)
	require.Equal(t, 0, m.pickingForIndex)

	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "main"})
	require.Equal(t, branchModeStep, m.activeStep)
	require.Equal(t, []string{"main"}, m.pickedBranches)

	specs := buildSpecs(m.selectedWorkspaces, m.pickedBranches, true)
	require.Equal(t, []provider.SessionWorkspaceSpec{{WorkspaceID: 42, BaseBranch: "main"}}, specs)
}

func TestForm_MultiWorkspaceFlow_IteratesBranchPickerPerWorkspace(t *testing.T) {
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
	require.Equal(t, branchPickerStep, m.activeStep)
	require.Equal(t, 0, m.pickingForIndex)

	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "main"})
	require.Equal(t, branchPickerStep, m.activeStep, "should still be picking, now for ws2")
	require.Equal(t, 1, m.pickingForIndex)

	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "develop"})
	require.Equal(t, branchModeStep, m.activeStep)
	require.Equal(t, []string{"main", "develop"}, m.pickedBranches)

	specs := buildSpecs(m.selectedWorkspaces, m.pickedBranches, false)
	require.Equal(t, []provider.SessionWorkspaceSpec{
		{WorkspaceID: 10, Branch: "main"},
		{WorkspaceID: 20, Branch: "develop"},
	}, specs)
}

func TestForm_ReInitAfterPreviousCreate_ResetsState(t *testing.T) {
	m := New()
	m.SetSize(80, 24)
	m, _ = m.Init()

	wsA := workspace.Workspace{Name: "first", Path: "/tmp/a", IsGitRepo: true}
	wsA.ID = 1
	m, _ = m.Update(provider.WorkspacesStateUpdatedMsg{Workspaces: []workspace.Workspace{wsA}})
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspace: wsA, Workspaces: []workspace.Workspace{wsA}})
	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "main"})
	require.Equal(t, []string{"main"}, m.pickedBranches)

	m, _ = m.Init()
	require.Nil(t, m.selectedWorkspaces, "Init must reset selectedWorkspaces")
	require.Nil(t, m.pickedBranches, "Init must reset pickedBranches")
	require.Equal(t, 0, m.pickingForIndex)
	require.Equal(t, workspacePickerStep, m.activeStep)
}
