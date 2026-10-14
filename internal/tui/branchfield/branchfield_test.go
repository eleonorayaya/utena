package branchfield

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
)

func TestResolveDefault_Order(t *testing.T) {
	assert.Equal(t, "develop", ResolveDefault("develop", "feature", []string{"main"}))
	assert.Equal(t, "feature", ResolveDefault("", "feature", []string{"main"}))
	assert.Equal(t, "trunk", ResolveDefault("", "", []string{"trunk", "main"}))
	assert.Equal(t, "main", ResolveDefault("", "", nil))
}

func TestRow_LoadedKeepsExplicitPick(t *testing.T) {
	r := NewRow(workspace.Workspace{}).Pick("feature")
	r = r.Loaded(provider.BranchesLoadedMsg{Branches: []string{"main"}, DefaultBranch: "main"})
	assert.Equal(t, "feature", r.Branch)
	assert.False(t, r.IsDefault())
	assert.False(t, r.Loading)
}

func TestRow_CheckedIgnoresOtherWorkspaces(t *testing.T) {
	ws := workspace.Workspace{}
	ws.ID = 1
	r, _ := NewRow(ws).StartManual()
	r.Checking = "x"
	r = r.Checked(provider.BranchExistsCheckedMsg{WorkspaceID: 2, Name: "x", ExistsLocal: true})
	assert.Equal(t, "x", r.Checking)
	r = r.Checked(provider.BranchExistsCheckedMsg{WorkspaceID: 1, Name: "x", ExistsRemote: true})
	assert.Equal(t, "x", r.Branch)
	assert.False(t, r.Editing)
}
