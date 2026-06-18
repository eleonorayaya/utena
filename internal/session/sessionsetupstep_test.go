package session

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/require"
)

func newSetupStepStore(t *testing.T) *SessionSetupStepStore {
	t.Helper()
	database := testdb.New(t, &SessionSetupStep{})
	return NewSessionSetupStepStore(database)
}

func TestSessionSetupStepStore_CreateAndList(t *testing.T) {
	store := newSetupStepStore(t)

	steps := []*SessionSetupStep{
		{SessionID: 7, Position: 0, Kind: StepKindCreateSessionDir, Label: "Create session directory", Status: SetupStepPending},
		{SessionID: 7, Position: 1, Kind: StepKindSetupWorktree, Label: "Setup workspace: a", Status: SetupStepPending},
	}
	require.NoError(t, store.Create(steps))
	require.NotZero(t, steps[0].ID)
	require.NotZero(t, steps[1].ID)

	listed, err := store.ListBySessionID(7)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, 0, listed[0].Position)
	require.Equal(t, 1, listed[1].Position)
	require.Equal(t, StepKindCreateSessionDir, listed[0].Kind)
}

func TestSessionSetupStepStore_Update(t *testing.T) {
	store := newSetupStepStore(t)
	step := &SessionSetupStep{SessionID: 1, Position: 0, Kind: StepKindSetupTmux, Label: "Setup tmux", Status: SetupStepPending}
	require.NoError(t, store.Create([]*SessionSetupStep{step}))

	step.Status = SetupStepFailed
	step.ErrorMsg = "boom"
	require.NoError(t, store.Update(step))

	listed, err := store.ListBySessionID(1)
	require.NoError(t, err)
	require.Len(t, listed, 1)
	require.Equal(t, SetupStepFailed, listed[0].Status)
	require.Equal(t, "boom", listed[0].ErrorMsg)
}

func TestSessionSetupStepStore_ResetAllForSession(t *testing.T) {
	store := newSetupStepStore(t)
	dep := uint(99)
	steps := []*SessionSetupStep{
		{SessionID: 3, Position: 0, Kind: StepKindCreateSessionDir, Label: "dir", Status: SetupStepDone},
		{SessionID: 3, Position: 1, Kind: StepKindSetupWorktree, Label: "ws", Status: SetupStepFailed, ErrorMsg: "x", DependsOnID: &dep},
	}
	require.NoError(t, store.Create(steps))

	require.NoError(t, store.ResetAllForSession(3))

	listed, err := store.ListBySessionID(3)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	for _, s := range listed {
		require.Equal(t, SetupStepPending, s.Status)
		require.Empty(t, s.ErrorMsg)
	}
	require.NotNil(t, listed[1].DependsOnID, "ResetAllForSession must preserve DependsOnID")
	require.Equal(t, uint(99), *listed[1].DependsOnID)
}

func TestSessionSetupStepStore_DeleteBySessionID(t *testing.T) {
	store := newSetupStepStore(t)
	require.NoError(t, store.Create([]*SessionSetupStep{
		{SessionID: 5, Position: 0, Kind: StepKindCreateSessionDir, Label: "dir", Status: SetupStepPending},
	}))

	require.NoError(t, store.DeleteBySessionID(5))

	listed, err := store.ListBySessionID(5)
	require.NoError(t, err)
	require.Empty(t, listed)
}
