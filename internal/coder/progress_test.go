package coder

import (
	"context"
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stepView struct {
	Kind   session.SetupStepKind
	Status session.SetupStepStatus
	Error  string
}

func viewSteps(steps []session.SessionSetupStep) []stepView {
	out := make([]stepView, len(steps))
	for i, st := range steps {
		out[i] = stepView{st.Kind, st.Status, st.ErrorMsg}
	}
	return out
}

func TestService_CreateRecordsStagesAndFollowerSteps(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(nil)
	h.follower.set(func(f *fakeFollower) {
		f.statuses = []session.SessionStatus{session.StatusCreating, session.StatusActive}
		f.steps = []session.SessionSetupStep{
			{Kind: "run_command", Label: "yarn install", Position: 1, Status: session.SetupStepDone},
			{Kind: "create_worktree", Label: "Create worktree", Position: 0, Status: session.SetupStepDone},
		}
	})

	require.NoError(t, h.svc.Create(context.Background(), session.RemoteCreate{SessionID: 9, Workspace: "ws", Repo: "acme/widgets", LeaderWorkspaceID: 5, Spec: session.WorkspaceBranchSpec{BaseBranch: "main"}}))

	history := h.sessions.stepHistory(9)
	require.NotEmpty(t, history)
	for _, st := range history[0] {
		assert.Equal(t, session.SetupStepPending, st.Status)
	}
	assert.Equal(t, []stepView{
		{stepBuild, session.SetupStepDone, ""},
		{stepAgent, session.SetupStepDone, ""},
		{stepToken, session.SetupStepDone, ""},
		{stepConnect, session.SetupStepDone, ""},
		{stepLink, session.SetupStepDone, ""},
		{stepSession, session.SetupStepDone, ""},
		{"create_worktree", session.SetupStepDone, ""},
		{"run_command", session.SetupStepDone, ""},
	}, viewSteps(history[len(history)-1]))
	for i := 1; i < len(history); i++ {
		assert.NotEqual(t, viewSteps(history[i-1]), viewSteps(history[i]), "unchanged steps are not written again")
	}
}

func TestService_CreateMarksTheFailedStage(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(func(c Command) ([]byte, error, bool) {
		if c.Name == "coder" && c.Args[0] == "create" {
			return nil, errors.New("template not found"), true
		}
		return nil, nil, false
	})

	require.Error(t, h.svc.Create(context.Background(), session.RemoteCreate{SessionID: 9, Workspace: "ws", Repo: "acme/widgets", Spec: session.WorkspaceBranchSpec{BaseBranch: "main"}}))

	history := h.sessions.stepHistory(9)
	require.NotEmpty(t, history)
	last := viewSteps(history[len(history)-1])
	assert.Equal(t, stepBuild, last[0].Kind)
	assert.Equal(t, session.SetupStepFailed, last[0].Status)
	assert.Contains(t, last[0].Error, "template not found")
	for _, st := range last[1:] {
		assert.Equal(t, session.SetupStepPending, st.Status)
	}
}
