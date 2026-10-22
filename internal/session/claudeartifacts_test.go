package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claudedata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeProject(t *testing.T, dataDir, cwd, id string) string {
	t.Helper()
	dir := filepath.Join(dataDir, "projects", claudedata.ProjectDirName(cwd))
	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(fmt.Sprintf(`{"cwd":%q}`+"\n", cwd)), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "file-history", id), 0o755))
	return dir
}

func addRootedSession(t *testing.T, store *SessionStore, name string, status SessionStatus, root string) *Session {
	t.Helper()
	sess := &Session{Name: name, Status: status, SessionRoot: root, LastUsedAt: time.Now()}
	require.NoError(t, store.Add(sess))
	return sess
}

func TestRemoveClaudeArtifacts_RemovesOnlyTheDeletedSessionsProjects(t *testing.T) {
	service, store, _, _, _, _ := setupSessionService(t)
	data := t.TempDir()
	service.SetClaudeDataDir(data)
	root := filepath.Join(service.sessionsRoot, "home")
	sibling := filepath.Join(service.sessionsRoot, "home-notifs")
	sess := addRootedSession(t, store, "home", StatusArchived, root)
	addRootedSession(t, store, "home-notifs", StatusArchived, sibling)

	own := claudeProject(t, data, root, "a1")
	checkout := claudeProject(t, data, filepath.Join(root, "repo"), "a2")
	other := claudeProject(t, data, sibling, "b1")

	service.removeClaudeArtifacts(sess)

	assert.NoDirExists(t, own)
	assert.NoDirExists(t, checkout)
	assert.NoDirExists(t, filepath.Join(data, "file-history", "a1"))
	assert.DirExists(t, other)
	assert.DirExists(t, filepath.Join(data, "file-history", "b1"))
}

func TestRemoveClaudeArtifacts_KeepsProjectsOfALiveSessionReusingTheRoot(t *testing.T) {
	service, store, _, _, _, _ := setupSessionService(t)
	data := t.TempDir()
	service.SetClaudeDataDir(data)
	root := filepath.Join(service.sessionsRoot, "reused")
	old := addRootedSession(t, store, "reused", StatusArchived, root)
	addRootedSession(t, store, "reused", StatusActive, root)
	dir := claudeProject(t, data, root, "r1")

	service.removeClaudeArtifacts(old)

	assert.DirExists(t, dir)
}

func TestSessionStore_LiveSessionRoots(t *testing.T) {
	_, store, _, _, _, _ := setupSessionService(t)
	self := addRootedSession(t, store, "self", StatusArchived, "/s/self")
	addRootedSession(t, store, "live", StatusActive, "/s/live")
	addRootedSession(t, store, "gone", StatusDeleted, "/s/gone")
	addRootedSession(t, store, "rootless", StatusActive, "")

	roots, err := store.LiveSessionRoots(self.ID)
	require.NoError(t, err)
	assert.Equal(t, []string{"/s/live"}, roots)
}
