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

func TestSweepClaudeArtifacts_RemovesOrphansOnly(t *testing.T) {
	service, store, _, _, _, _ := setupSessionService(t)
	data := t.TempDir()
	service.SetClaudeDataDir(data)
	old := time.Now().Add(-time.Hour)

	activeRoot := filepath.Join(service.sessionsRoot, "active")
	archivedRoot := filepath.Join(service.sessionsRoot, "archived")
	deletedRoot := filepath.Join(service.sessionsRoot, "gone")
	addRootedSession(t, store, "active", StatusActive, activeRoot)
	addRootedSession(t, store, "archived", StatusArchived, archivedRoot)
	addRootedSession(t, store, "gone", StatusDeleted, deletedRoot)

	active := claudeProject(t, data, activeRoot, "s1", old)
	archived := claudeProject(t, data, archivedRoot, "s2", old)
	deleted := claudeProject(t, data, deletedRoot, "s3", old)
	unknown := claudeProject(t, data, filepath.Join(service.sessionsRoot, "never-recorded", "repo"), "s4", old)
	recent := claudeProject(t, data, filepath.Join(service.sessionsRoot, "just-created"), "s5", time.Now())
	outside := claudeProject(t, data, filepath.Join(t.TempDir(), "elsewhere"), "s6", old)
	memoryOnly := filepath.Join(data, "projects", "memory-only")
	require.NoError(t, os.MkdirAll(filepath.Join(memoryOnly, "memory"), 0o755))

	service.sweepClaudeArtifacts()

	assert.NoDirExists(t, deleted)
	assert.NoDirExists(t, unknown)
	assert.DirExists(t, active)
	assert.DirExists(t, archived)
	assert.DirExists(t, recent)
	assert.DirExists(t, outside)
	assert.DirExists(t, memoryOnly)
}

func TestSweepClaudeArtifacts_NoopWithoutDataDir(t *testing.T) {
	service, _, _, _, _, _ := setupSessionService(t)
	service.sweepClaudeArtifacts()
}
