package session

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
)

type fakeSource struct {
	data string
	mode fs.FileMode
	err  error
}

type fakeSourceReader struct {
	mu          sync.Mutex
	files       map[string]fakeSource
	global      *fakeSource
	configReads int
	failConfig  func(read int) error
	workspaces  []workspace.Workspace
}

func (f *fakeSourceReader) ReadSourceFile(_ context.Context, ws workspace.Workspace, relPath string) ([]byte, fs.FileMode, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.workspaces = append(f.workspaces, ws)
	if relPath == workspaceConfigRelPath {
		f.configReads++
		if f.failConfig != nil {
			if err := f.failConfig(f.configReads); err != nil {
				return nil, 0, err
			}
		}
	}
	src, ok := f.files[relPath]
	if !ok {
		return nil, 0, fmt.Errorf("%s: %w", relPath, fs.ErrNotExist)
	}
	if src.err != nil {
		return nil, 0, src.err
	}
	return []byte(src.data), src.mode, nil
}

func (f *fakeSourceReader) ReadGlobalSetupScript(context.Context) ([]byte, fs.FileMode, error) {
	if f.global == nil {
		return nil, 0, fmt.Errorf("global: %w", fs.ErrNotExist)
	}
	if f.global.err != nil {
		return nil, 0, f.global.err
	}
	return []byte(f.global.data), f.global.mode, nil
}

var errLeaderDown = errors.New("leader unreachable: connection refused")

func setupRemoteSourceService(t *testing.T, reader *fakeSourceReader) (*SessionService, *SessionStore, uint) {
	t.Helper()
	repoPath := initTestRepo(t)
	service, sessionStore, _, wsID := setupWorktreeSessionService(t, repoPath, t.TempDir())
	service.SetSourceReader(reader)
	return service, sessionStore, wsID
}

func createRemoteSession(t *testing.T, service *SessionService, wsID uint, name string) (*Session, error) {
	t.Helper()
	return service.CreateSession(context.Background(), CreateSessionInput{
		Name:       name,
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: wsID, BaseBranch: "main"}},
	})
}

func requireBroken(t *testing.T, store *SessionStore, id uint, contains string) *Session {
	t.Helper()
	waitForStatus(t, store, id, StatusBroken, 5*time.Second)
	final, err := store.GetByID(id)
	require.NoError(t, err)
	require.Contains(t, final.StatusError, contains)
	return final
}

func TestSessionService_RemoteSources_RunsSetupFromReader(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath:  {data: `{"setup_actions":[{"type":"copy_file","src":".env","dest":".env","dest_relative_to":"workspace_dir"}],"default_bundles":["go"]}`, mode: 0o644},
		".env":                  {data: "A=1\n", mode: 0o600},
		".utena/worktree-setup": {data: "#!/bin/sh\necho \"$0\" > ws-ran\n", mode: 0o755},
	}, global: &fakeSource{data: "#!/bin/sh\ntouch global-ran\n", mode: 0o750}}
	service, store, wsID := setupRemoteSourceService(t, reader)
	require.NoError(t, store.db.Model(&workspace.Workspace{}).Where("id = ?", wsID).Update("leader_workspace_id", 5).Error)

	sess, err := createRemoteSession(t, service, wsID, "remote-ok")
	require.NoError(t, err)
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	final, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Empty(t, final.StatusError)
	require.Equal(t, []string{"go"}, final.Bundles)

	worktree := filepath.Join(service.sessionsRoot, "remote-ok", "git-repo")
	data, err := os.ReadFile(filepath.Join(worktree, ".env"))
	require.NoError(t, err)
	require.Equal(t, "A=1\n", string(data))

	scriptPath, err := os.ReadFile(filepath.Join(worktree, "ws-ran"))
	require.NoError(t, err)
	staged := filepath.Clean(string(scriptPath[:len(scriptPath)-1]))
	_, statErr := os.Stat(staged)
	require.ErrorIs(t, statErr, fs.ErrNotExist, "staged script should be removed after it runs")

	_, err = os.Stat(filepath.Join(service.sessionsRoot, "remote-ok", "global-ran"))
	require.NoError(t, err)

	require.NotEmpty(t, reader.workspaces)
	for _, ws := range reader.workspaces {
		require.Equal(t, wsID, ws.ID)
		require.NotNil(t, ws.LeaderWorkspaceID)
		require.Equal(t, uint(5), *ws.LeaderWorkspaceID)
	}
}

func TestSessionService_RemoteSources_MissingFilesAreNothingToDo(t *testing.T) {
	reader := &fakeSourceReader{}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-empty")
	require.NoError(t, err)
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	final, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Empty(t, final.StatusError)
}

func TestSessionService_RemoteSources_ConfigUnreachableFailsCreate(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath: {err: errLeaderDown},
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	_, err := createRemoteSession(t, service, wsID, "remote-config-down")
	require.Error(t, err)
	require.ErrorIs(t, err, errLeaderDown)

	sessions, listErr := store.List()
	require.NoError(t, listErr)
	require.Empty(t, sessions)
}

func TestSessionService_RemoteSources_ConfigUnreachableDuringSetupBreaks(t *testing.T) {
	reader := &fakeSourceReader{failConfig: func(read int) error {
		if read > 1 {
			return errLeaderDown
		}
		return nil
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-config-late")
	require.NoError(t, err)
	requireBroken(t, store, sess.ID, "connection refused")
}

func TestSessionService_RemoteSources_CopySourceUnreachableBreaks(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath: {data: `{"setup_actions":[{"type":"copy_file","src":".env","dest":".env"}]}`, mode: 0o644},
		".env":                 {err: errLeaderDown},
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-copy-down")
	require.NoError(t, err)
	requireBroken(t, store, sess.ID, "connection refused")

	steps, err := service.setupStepStore.ListBySessionID(sess.ID)
	require.NoError(t, err)
	copyStep := stepByKind(t, steps, StepKindCopyFile)
	require.NotNil(t, copyStep)
	require.Equal(t, SetupStepFailed, copyStep.Status)
}

func TestSessionService_RemoteSources_CopySourceMissingIsAWarning(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath: {data: `{"setup_actions":[{"type":"copy_file","src":".env","dest":".env"}]}`, mode: 0o644},
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-copy-missing")
	require.NoError(t, err)
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	final, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Contains(t, final.StatusError, "copy_file source not found")
}

func TestSessionService_RemoteSources_WorkspaceScriptUnreachableBreaks(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		".utena/worktree-setup": {err: errLeaderDown},
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-script-down")
	require.NoError(t, err)
	requireBroken(t, store, sess.ID, "connection refused")
}

func TestSessionService_RemoteSources_GlobalScriptUnreachableBreaks(t *testing.T) {
	reader := &fakeSourceReader{global: &fakeSource{err: errLeaderDown}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-global-down")
	require.NoError(t, err)
	requireBroken(t, store, sess.ID, "connection refused")
}

func TestSessionService_RemoteSources_NonExecutableScriptIsAWarning(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		".utena/worktree-setup": {data: "#!/bin/sh\ntouch ran\n", mode: 0o644},
	}}
	service, store, wsID := setupRemoteSourceService(t, reader)

	sess, err := createRemoteSession(t, service, wsID, "remote-noexec")
	require.NoError(t, err)
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	final, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	require.Contains(t, final.StatusError, "not executable")
	_, statErr := os.Stat(filepath.Join(service.sessionsRoot, "remote-noexec", "git-repo", "ran"))
	require.ErrorIs(t, statErr, fs.ErrNotExist)
}

func TestSessionService_RemoteSources_DefaultBundlesForPropagatesReadErrors(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath: {err: errLeaderDown},
	}}
	service, _, wsID := setupRemoteSourceService(t, reader)

	_, err := service.DefaultBundlesFor(context.Background(), []uint{wsID})
	require.ErrorIs(t, err, errLeaderDown)
}

func TestSessionService_RemoteSources_DefaultBundlesForSkipsMalformedConfig(t *testing.T) {
	reader := &fakeSourceReader{files: map[string]fakeSource{
		workspaceConfigRelPath: {data: `{`, mode: 0o644},
	}}
	service, _, wsID := setupRemoteSourceService(t, reader)

	got, err := service.DefaultBundlesFor(context.Background(), []uint{wsID})
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestSessionService_CreateSession_StoresLeaderSessionID(t *testing.T) {
	service, store, wsID := setupRemoteSourceService(t, &fakeSourceReader{})
	leaderID := uint(77)

	sess, err := service.CreateSession(context.Background(), CreateSessionInput{
		Name:            "with-leader",
		Workspaces:      []WorkspaceBranchSpec{{WorkspaceID: wsID, BaseBranch: "main"}},
		LeaderSessionID: &leaderID,
	})
	require.NoError(t, err)
	waitForStatus(t, store, sess.ID, StatusActive, 5*time.Second)

	got, err := service.LeaderSessionID(sess.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint(77), *got)

	_, err = service.LeaderSessionID(99999)
	require.ErrorIs(t, err, ErrSessionNotFound)
}
