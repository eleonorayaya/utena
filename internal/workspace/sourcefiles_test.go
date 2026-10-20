package workspace

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRelPath(t *testing.T) {
	assert.NoError(t, ValidateRelPath(".utena/config.json"))
	assert.NoError(t, ValidateRelPath(".env"))
	assert.Error(t, ValidateRelPath(""))
	assert.Error(t, ValidateRelPath("/etc/passwd"))
	assert.Error(t, ValidateRelPath("../secret"))
	assert.Error(t, ValidateRelPath("a/../../b"))
}

func TestLocalSourceReader_ReadsFileWithMode(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".utena"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".utena", "worktree-setup"), []byte("#!/bin/sh\n"), 0o755))

	r := NewLocalSourceReader(t.TempDir())
	data, mode, err := r.ReadSourceFile(context.Background(), Workspace{Path: dir}, ".utena/worktree-setup")
	require.NoError(t, err)
	assert.Equal(t, "#!/bin/sh\n", string(data))
	assert.Equal(t, fs.FileMode(0o755), mode)
}

func TestLocalSourceReader_MissingIsNotExist(t *testing.T) {
	r := NewLocalSourceReader(t.TempDir())

	_, _, err := r.ReadSourceFile(context.Background(), Workspace{Path: t.TempDir()}, ".utena/config.json")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	_, _, err = r.ReadGlobalSetupScript(context.Background())
	assert.ErrorIs(t, err, fs.ErrNotExist)

	_, _, err = NewLocalSourceReader("").ReadGlobalSetupScript(context.Background())
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLocalSourceReader_RejectsTraversal(t *testing.T) {
	r := NewLocalSourceReader(t.TempDir())

	_, _, err := r.ReadSourceFile(context.Background(), Workspace{Path: t.TempDir()}, "../x")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLocalSourceReader_DirectoryIsAnError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "sub"), 0o755))

	_, _, err := NewLocalSourceReader(t.TempDir()).ReadSourceFile(context.Background(), Workspace{Path: dir}, "sub")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLocalSourceReader_ReadsGlobalSetupScript(t *testing.T) {
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, GlobalSetupScriptName), []byte("echo hi"), 0o700))

	data, mode, err := NewLocalSourceReader(configDir).ReadGlobalSetupScript(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "echo hi", string(data))
	assert.Equal(t, fs.FileMode(0o700), mode)
}

func setupFilesRouter(t *testing.T) (http.Handler, *Workspace, string) {
	t.Helper()
	database := testdb.New(t, &Workspace{}, &git.Repo{}, &git.Branch{}, &git.Worktree{}, &git.PullRequest{})
	configDir := t.TempDir()
	store := NewWorkspaceStore(database, afero.NewMemMapFs(), configDir)

	wsDir := t.TempDir()
	ws := &Workspace{Name: "repo", Path: wsDir, IsGitRepo: true}
	require.NoError(t, store.Add(ws))

	gitService := git.NewGitService(database)
	router := NewWorkspaceRouter(NewWorkspaceController(NewWorkspaceService(store, gitService), store, gitService))
	return router.Routes(), ws, configDir
}

func TestWorkspaceRouter_GetSourceFile(t *testing.T) {
	handler, ws, _ := setupFilesRouter(t)
	require.NoError(t, os.MkdirAll(filepath.Join(ws.Path, ".utena"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws.Path, ".utena", "worktree-setup"), []byte("#!/bin/sh\necho ok\n"), 0o750))

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%d/files/.utena/worktree-setup", ws.ID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "#!/bin/sh\necho ok\n", w.Body.String())
	assert.Equal(t, "0750", w.Header().Get(FileModeHeader))
	assert.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))
}

func TestWorkspaceRouter_GetSourceFile_Missing(t *testing.T) {
	handler, ws, _ := setupFilesRouter(t)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%d/files/.utena/config.json", ws.ID), nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotEmpty(t, w.Header().Get(FileMissingHeader))
}

func TestWorkspaceRouter_GetSourceFile_UnknownWorkspaceIsUnmarked404(t *testing.T) {
	handler, _, _ := setupFilesRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/9999/files/.env", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, w.Header().Get(FileMissingHeader))
}

func TestWorkspaceRouter_GetSourceFile_RejectsTraversal(t *testing.T) {
	handler, ws, _ := setupFilesRouter(t)

	for _, p := range []string{"../outside", "a/../../outside", "%2e%2e/outside", ""} {
		req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/%d/files/%s", ws.ID, p), nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, p)
	}
}

func TestWorkspaceRouter_GetGlobalSetupScript(t *testing.T) {
	handler, _, configDir := setupFilesRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/"+GlobalSetupScriptName, nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NotEmpty(t, w.Header().Get(FileMissingHeader))

	require.NoError(t, os.WriteFile(filepath.Join(configDir, GlobalSetupScriptName), []byte("#!/bin/sh\n"), 0o755))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+GlobalSetupScriptName, nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "#!/bin/sh\n", w.Body.String())
	assert.Equal(t, "0755", w.Header().Get(FileModeHeader))
}
