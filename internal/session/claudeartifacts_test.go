package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func claudeProject(t *testing.T, dataDir, cwd, id string, modTime time.Time) string {
	t.Helper()
	dir := filepath.Join(dataDir, "projects", filepath.Base(filepath.Dir(cwd))+"-"+filepath.Base(cwd)+"-"+id)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, id+".jsonl")
	require.NoError(t, os.WriteFile(file, []byte(fmt.Sprintf(`{"type":"mode"}`+"\n"+`{"cwd":%q}`+"\n", cwd)), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "file-history", id), 0o755))
	require.NoError(t, os.Chtimes(file, modTime, modTime))
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

	old := time.Now().Add(-time.Hour)
	own := claudeProject(t, data, root, "a1", old)
	checkout := claudeProject(t, data, filepath.Join(root, "repo"), "a2", time.Now())
	other := claudeProject(t, data, sibling, "b1", old)

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
	dir := claudeProject(t, data, root, "r1", time.Now().Add(-time.Hour))

	service.removeClaudeArtifacts(old)

	assert.DirExists(t, dir)
}
