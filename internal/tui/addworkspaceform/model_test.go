package addworkspaceform

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func gitWS(id uint, name string) workspace.Workspace {
	w := workspace.Workspace{Name: name, IsGitRepo: true}
	w.ID = id
	return w
}

func started(t *testing.T) Model {
	t.Helper()
	m, _ := New().Init()
	m.SetSize(80, 20)
	m, _ = m.Update(StartMsg{SessionID: 9})
	return m
}

func TestAddWorkspace_OpensIntoPickerAndFocusesMode(t *testing.T) {
	m := started(t)
	assert.Equal(t, overlayWorkspace, m.overlay)
	m, cmd := m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{gitWS(3, "web")}})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, fieldMode, m.form.Focused())
	assert.Equal(t, "provider.requestBranchesMsg", reflect.TypeOf(cmd()).String())
	assert.Contains(t, m.View(), "web")
}

func TestAddWorkspace_EnterOnModeSubmitsWithDefaultBranch(t *testing.T) {
	m := started(t)
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{gitWS(3, "web")}})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "blocked while loading")
	m, _ = m.Update(provider.BranchesLoadedMsg{WorkspaceID: 3, Branches: []string{"main"}, DefaultBranch: "main"})
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	assert.Equal(t, "provider.addWorkspaceToSessionIntentMsg", reflect.TypeOf(cmd()).String())
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "repeated submits are ignored")
}

func TestAddWorkspace_BranchPickerRoundTrip(t *testing.T) {
	m := started(t)
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{gitWS(3, "web")}})
	m, _ = m.Update(provider.BranchesLoadedMsg{WorkspaceID: 3, Branches: []string{"main", "dev"}, DefaultBranch: "main"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.Equal(t, overlayBranch, m.overlay)
	m, _ = m.Update(branchpicker.SelectedMsg{Branch: "dev"})
	assert.Equal(t, overlayNone, m.overlay)
	assert.Equal(t, "dev", m.row.Branch)
}

func TestAddWorkspace_NonGitStaysInPicker(t *testing.T) {
	m := started(t)
	m, _ = m.Update(workspacepicker.SelectedMsg{Workspaces: []workspace.Workspace{{Name: "notes"}}})
	assert.Equal(t, overlayWorkspace, m.overlay)
	assert.Contains(t, m.Status().Text, "not a git repository")
}

func TestAddWorkspace_PickerDoesNotOfferAddDir(t *testing.T) {
	m := started(t)
	for _, b := range m.Keys().ShortHelp() {
		if b.Help().Key == "a" {
			assert.False(t, b.Enabled())
		}
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if cmd != nil {
		assert.NotEqual(t, workspacepicker.AddDirectoryMsg{}, cmd())
	}
}
