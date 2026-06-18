package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func stepByKind(t *testing.T, steps []*SessionSetupStep, kind SetupStepKind) *SessionSetupStep {
	t.Helper()
	for _, s := range steps {
		if s.Kind == kind {
			return s
		}
	}
	return nil
}

func TestSessionService_SetupSteps_PrePopulated(t *testing.T) {
	repoPath := initTestRepo(t)
	service, _, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "steps-prepop",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)

	kinds := make([]SetupStepKind, len(steps))
	for i, s := range steps {
		kinds[i] = s.Kind
	}
	require.Equal(t, []SetupStepKind{
		StepKindCreateSessionDir,
		StepKindSetupWorktree,
		StepKindApplyClaude,
		StepKindSetupTmux,
	}, kinds)
}

func TestSessionService_SetupSteps_AllComplete(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "steps-complete",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, steps)
	for _, s := range steps {
		require.Equal(t, SetupStepDone, s.Status, "step %q should be done", s.Label)
	}
}

func TestSessionService_ConfigAction_CopyFile(t *testing.T) {
	repoPath := initTestRepo(t)
	require.NoError(t, os.WriteFile(filepath.Join(repoPath, ".env.example"), []byte("SECRET=1\n"), 0o644))
	writeWorkspaceConfig(t, repoPath, `{
		"setup_actions": [
			{"type": "copy_file", "src": ".env.example", "dest": ".env", "dest_relative_to": "workspace_dir"},
			{"type": "copy_file", "src": ".env.example", "dest": "shared/config.env", "dest_relative_to": "session_root"}
		]
	}`)

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "copy-test",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	workspaceDir := filepath.Join(service.sessionsRoot, "copy-test", "git-repo")
	data, err := os.ReadFile(filepath.Join(workspaceDir, ".env"))
	require.NoError(t, err)
	require.Equal(t, "SECRET=1\n", string(data))

	sessionRootCopy := filepath.Join(service.sessionsRoot, "copy-test", "shared", "config.env")
	data, err = os.ReadFile(sessionRootCopy)
	require.NoError(t, err)
	require.Equal(t, "SECRET=1\n", string(data))

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	copyStep := stepByKind(t, steps, StepKindCopyFile)
	require.NotNil(t, copyStep)
	require.Equal(t, SetupStepDone, copyStep.Status)
}

func TestSessionService_ConfigAction_InstallDeps(t *testing.T) {
	repoPath := initTestRepo(t)
	pmScript := filepath.Join(t.TempDir(), "fake-pm")
	writeScript(t, pmScript, "#!/bin/sh\ntouch dep-installed\n")
	writeWorkspaceConfig(t, repoPath, `{
		"setup_actions": [
			{"type": "install_deps", "package_manager": "`+pmScript+`"}
		]
	}`)

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "install-test",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	marker := filepath.Join(service.sessionsRoot, "install-test", "git-repo", "dep-installed")
	_, err = os.Stat(marker)
	require.NoError(t, err, "install_deps should have run in the worktree dir")

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	installStep := stepByKind(t, steps, StepKindInstallDeps)
	require.NotNil(t, installStep)
	require.Equal(t, SetupStepDone, installStep.Status)
}

func TestSessionService_ConfigAction_InstallDeps_Parallel(t *testing.T) {
	service, sessionStore, _, ws1ID, ws2ID := setupTwoRepoSessionService(t)

	pmScript := filepath.Join(t.TempDir(), "fake-pm")
	writeScript(t, pmScript, "#!/bin/sh\ntouch dep-installed\n")
	cfg := `{"setup_actions": [{"type": "install_deps", "package_manager": "` + pmScript + `"}]}`

	ws1, err := service.workspaceService.GetWorkspace(context.Background(), ws1ID)
	require.NoError(t, err)
	ws2, err := service.workspaceService.GetWorkspace(context.Background(), ws2ID)
	require.NoError(t, err)
	writeWorkspaceConfig(t, ws1.Path, cfg)
	writeWorkspaceConfig(t, ws2.Path, cfg)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name: "parallel-install",
		Workspaces: []WorkspaceBranchSpec{
			{WorkspaceID: ws1ID, BaseBranch: "main"},
			{WorkspaceID: ws2ID, BaseBranch: "main"},
		},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 10*time.Second)

	for _, sub := range []string{"repo1", "repo2"} {
		marker := filepath.Join(service.sessionsRoot, "parallel-install", sub, "dep-installed")
		_, statErr := os.Stat(marker)
		require.NoError(t, statErr, "install_deps should have run in %s", sub)
	}

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	installCount := 0
	for _, s := range steps {
		if s.Kind == StepKindInstallDeps {
			installCount++
			require.Equal(t, SetupStepDone, s.Status)
		}
	}
	require.Equal(t, 2, installCount, "one install_deps step per workspace")
}

func TestSessionService_ConfigAction_CopyFailsInstallContinues(t *testing.T) {
	repoPath := initTestRepo(t)
	pmScript := filepath.Join(t.TempDir(), "fake-pm")
	writeScript(t, pmScript, "#!/bin/sh\ntouch dep-installed\n")
	writeWorkspaceConfig(t, repoPath, `{
		"setup_actions": [
			{"type": "copy_file", "src": "does-not-exist", "dest": ".env", "dest_relative_to": "workspace_dir"},
			{"type": "install_deps", "package_manager": "`+pmScript+`"}
		]
	}`)

	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "copy-fail",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	final, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	require.Equal(t, StatusActive, final.Status)
	require.NotEmpty(t, final.StatusError, "failed copy should surface as a warning")

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	copyStep := stepByKind(t, steps, StepKindCopyFile)
	require.NotNil(t, copyStep)
	require.Equal(t, SetupStepFailed, copyStep.Status)

	installStep := stepByKind(t, steps, StepKindInstallDeps)
	require.NotNil(t, installStep)
	require.Equal(t, SetupStepDone, installStep.Status, "install_deps should still run after a failed copy_file")

	marker := filepath.Join(service.sessionsRoot, "copy-fail", "git-repo", "dep-installed")
	_, statErr := os.Stat(marker)
	require.NoError(t, statErr)
}

func TestSessionService_RepairSession_StepsReset(t *testing.T) {
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsGitID := setupWorktreeSessionService(t, repoPath, t.TempDir())

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:       "repair-steps",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsGitID, BaseBranch: "main"}},
	})
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	before, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	require.NotEmpty(t, before)

	reloaded, err := sessionStore.GetByID(sess.ID)
	require.NoError(t, err)
	reloaded.Status = StatusBroken
	reloaded.StatusError = "forced for test"
	require.NoError(t, sessionStore.Update(reloaded))

	_, err = service.RepairSession(context.Background(), sess.ID)
	require.NoError(t, err)
	waitForStatus(t, sessionStore, sess.ID, StatusActive, 5*time.Second)

	after, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	require.Len(t, after, len(before), "repair should not change step count")
	for _, s := range after {
		require.Equal(t, SetupStepDone, s.Status, "step %q should be done after repair", s.Label)
	}
}
