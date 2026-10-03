package tmux

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func workspaces(current int, counts ...int) []workspaceInfo {
	out := make([]workspaceInfo, len(counts))
	for i, c := range counts {
		out[i] = workspaceInfo{Workspace: i + 1, WindowCount: c, Current: i+1 == current}
	}
	return out
}

func TestStepWorkspace_CyclesThroughUsedWorkspacesWithWrap(t *testing.T) {
	ws := workspaces(2, 1, 1, 0, 3, 0, 0, 0, 0, 0)
	assert.Equal(t, 4, stepWorkspace(ws, 1), "skips empty 3")
	assert.Equal(t, 1, stepWorkspace(ws, -1))

	ws = workspaces(4, 1, 1, 0, 3, 0, 0, 0, 0, 0)
	assert.Equal(t, 1, stepWorkspace(ws, 1), "wraps forward")
	ws = workspaces(1, 1, 1, 0, 3, 0, 0, 0, 0, 0)
	assert.Equal(t, 4, stepWorkspace(ws, -1), "wraps backward")
}

func TestStepWorkspace_EmptyCurrentStillCycles(t *testing.T) {
	ws := workspaces(3, 1, 0, 0, 0, 2, 0, 0, 0, 0)
	assert.Equal(t, 5, stepWorkspace(ws, 1))
	assert.Equal(t, 1, stepWorkspace(ws, -1))
}

func TestStepWorkspace_OnlyCurrentStays(t *testing.T) {
	ws := workspaces(1, 2, 0, 0, 0, 0, 0, 0, 0, 0)
	assert.Equal(t, 1, stepWorkspace(ws, 1))
}

func TestPickEmptyWorkspace_PrefersAfterCurrentThenWraps(t *testing.T) {
	n, ok := pickEmptyWorkspace(workspaces(2, 1, 1, 1, 0, 0, 0, 0, 0, 0))
	require.True(t, ok)
	assert.Equal(t, 4, n)

	n, ok = pickEmptyWorkspace(workspaces(8, 1, 0, 1, 1, 1, 1, 1, 1, 1))
	require.True(t, ok)
	assert.Equal(t, 2, n, "wraps to the lowest empty slot")

	_, ok = pickEmptyWorkspace(workspaces(1, 1, 1, 1, 1, 1, 1, 1, 1, 1))
	assert.False(t, ok)
}

func TestNewWorkspaceWindow_OpensInEmptyWorkspaceAndShowsIt(t *testing.T) {
	r, d := startFakeTuios(t, map[string]string{
		"list-workspaces":  `{"id":1,"result":{"workspaces":[{"workspace":1,"window_count":2,"current":true},{"workspace":2,"window_count":0}]}}`,
		"new-window":       `{"id":1,"result":{"type":"window_created"}}`,
		"select-workspace": `{"id":1,"result":{"type":"ok"}}`,
	})
	t.Setenv("TUIOS_SOCKET", r.socketPath)

	require.NoError(t, NewWorkspaceWindow("work", "/src"))

	d.mu.Lock()
	defer d.mu.Unlock()
	require.Len(t, d.requests, 3)
	assert.Equal(t, "new-window", d.requests[1].Verb)
	assert.Equal(t, float64(2), d.requests[1].Params["workspace"])
	assert.Equal(t, "/src", d.requests[1].Params["cwd"])
	assert.Equal(t, "select-workspace", d.requests[2].Verb)
	assert.Equal(t, float64(2), d.requests[2].Params["workspace"])
}
