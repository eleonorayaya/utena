package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func kinds(defs []stepDef) []SetupStepKind {
	out := make([]SetupStepKind, len(defs))
	for i, d := range defs {
		out[i] = d.Kind
	}
	return out
}

func TestBuildSetupSteps_SingleWorkspaceNoConfig(t *testing.T) {
	defs := buildSetupSteps([]stepWorkspace{{ID: 1, Name: "a", Path: "/a"}}, nil)

	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindApplyClaude,
		StepKindSetupTmux,
	}, kinds(defs))

	require.Nil(t, defs[0].DependsOnPos)
	require.Equal(t, 0, *defs[1].DependsOnPos)
	require.Nil(t, defs[2].DependsOnPos, "apply_claude has no explicit dependency")
	require.Equal(t, 2, *defs[3].DependsOnPos, "setup_tmux depends on apply_claude")
}

func TestBuildSetupSteps_CopyFilesChain(t *testing.T) {
	configs := map[uint]*WorkspaceConfig{
		1: {SetupActions: []WorkspaceSetupAction{
			{Type: WorkspaceActionCopyFile, Src: ".env.example", Dest: ".env", DestRelativeTo: DestRelativeToWorkspaceDir},
			{Type: WorkspaceActionCopyFile, Src: "a", Dest: "b"},
		}},
	}
	defs := buildSetupSteps([]stepWorkspace{{ID: 1, Name: "a"}}, configs)

	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindCopyFile,
		StepKindCopyFile,
		StepKindApplyClaude,
		StepKindSetupTmux,
	}, kinds(defs))

	require.Equal(t, 1, *defs[2].DependsOnPos, "first copy depends on setup_worktree")
	require.Equal(t, 2, *defs[3].DependsOnPos, "second copy depends on first copy")
	require.Equal(t, ".env.example", defs[2].CopySrc)
	require.Equal(t, DestRelativeToWorkspaceDir, defs[2].CopyDestRelTo)
}

func TestBuildSetupSteps_InstallDepsAfterCopy(t *testing.T) {
	configs := map[uint]*WorkspaceConfig{
		1: {SetupActions: []WorkspaceSetupAction{
			{Type: WorkspaceActionInstallDeps, PackageManager: packageManagerAuto},
			{Type: WorkspaceActionCopyFile, Src: ".env.example", Dest: ".env"},
		}},
	}
	defs := buildSetupSteps([]stepWorkspace{{ID: 1, Name: "a"}}, configs)

	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindCopyFile,
		StepKindInstallDeps,
		StepKindApplyClaude,
		StepKindSetupTmux,
	}, kinds(defs), "copy_file always emitted before install_deps regardless of config order")

	require.Equal(t, 1, *defs[2].DependsOnPos, "copy depends on setup_worktree")
	require.Equal(t, 2, *defs[3].DependsOnPos, "install_deps depends on the copy_file")
}

func TestBuildSetupSteps_MultiWorkspaceParallelChains(t *testing.T) {
	defs := buildSetupSteps([]stepWorkspace{
		{ID: 1, Name: "a"},
		{ID: 2, Name: "b"},
	}, nil)

	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindSetupWorktree,
		StepKindApplyClaude,
		StepKindSetupTmux,
	}, kinds(defs))

	require.Equal(t, 0, *defs[1].DependsOnPos, "workspace a setup depends on session dir")
	require.Equal(t, 0, *defs[2].DependsOnPos, "workspace b setup also depends on session dir (parallel)")
	require.Equal(t, uint(1), *defs[1].WorkspaceID)
	require.Equal(t, uint(2), *defs[2].WorkspaceID)
	require.Nil(t, defs[3].DependsOnPos)
}

func TestBuildSetupSteps_InstallDepsLabelWithExplicitPM(t *testing.T) {
	configs := map[uint]*WorkspaceConfig{
		1: {SetupActions: []WorkspaceSetupAction{
			{Type: WorkspaceActionInstallDeps, PackageManager: packageManagerYarn},
		}},
	}
	defs := buildSetupSteps([]stepWorkspace{{ID: 1, Name: "web"}}, configs)
	require.Equal(t, "Install dependencies: web (yarn)", defs[2].Label)
}
