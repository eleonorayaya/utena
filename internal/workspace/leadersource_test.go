package workspace

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func leaderWorkspace(id uint) Workspace {
	return Workspace{Name: "repo", LeaderWorkspaceID: &id}
}

func TestLeaderSourceReader_ReadsFileWithMode(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set(FileModeHeader, "0755")
		_, _ = w.Write([]byte("#!/bin/sh\n"))
	}))
	defer srv.Close()

	data, mode, err := NewLeaderSourceReader(srv.URL+"/", nil).ReadSourceFile(context.Background(), leaderWorkspace(7), ".utena/worktree-setup")
	require.NoError(t, err)
	assert.Equal(t, "/workspaces/7/files/.utena/worktree-setup", gotPath)
	assert.Equal(t, "#!/bin/sh\n", string(data))
	assert.Equal(t, fs.FileMode(0o755), mode)
}

func TestLeaderSourceReader_EscapesPathSegments(t *testing.T) {
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRaw = r.URL.EscapedPath()
		w.Header().Set(FileModeHeader, "0644")
	}))
	defer srv.Close()

	_, _, err := NewLeaderSourceReader(srv.URL, nil).ReadSourceFile(context.Background(), leaderWorkspace(1), "dir/a b#c.txt")
	require.NoError(t, err)
	assert.Equal(t, "/workspaces/1/files/dir/a%20b%23c.txt", gotRaw)
}

func TestLeaderSourceReader_MarkedNotFoundIsNotExist(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(FileMissingHeader, "1")
		http.Error(w, "file not found", http.StatusNotFound)
	}))
	defer srv.Close()

	r := NewLeaderSourceReader(srv.URL, nil)
	_, _, err := r.ReadSourceFile(context.Background(), leaderWorkspace(1), ".utena/config.json")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	_, _, err = r.ReadGlobalSetupScript(context.Background())
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestLeaderSourceReader_UnmarkedNotFoundIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, _, err := NewLeaderSourceReader(srv.URL, nil).ReadSourceFile(context.Background(), leaderWorkspace(1), ".utena/config.json")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
	assert.Contains(t, err.Error(), "404")
}

func TestLeaderSourceReader_ServerErrorIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, _, err := NewLeaderSourceReader(srv.URL, nil).ReadSourceFile(context.Background(), leaderWorkspace(1), ".env")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
	assert.Contains(t, err.Error(), "boom")
}

func TestLeaderSourceReader_MissingModeHeaderIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer srv.Close()

	_, _, err := NewLeaderSourceReader(srv.URL, nil).ReadSourceFile(context.Background(), leaderWorkspace(1), ".env")
	require.Error(t, err)
	assert.Contains(t, err.Error(), FileModeHeader)
}

func TestLeaderSourceReader_UnreachableIsNotNotExist(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	r := NewLeaderSourceReader(url, nil)
	_, _, err := r.ReadSourceFile(context.Background(), leaderWorkspace(1), ".utena/config.json")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
	assert.Contains(t, err.Error(), "leader unreachable")

	_, _, err = r.ReadGlobalSetupScript(context.Background())
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLeaderSourceReader_TimeoutIsNotNotExist(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	r := NewLeaderSourceReader(srv.URL, &http.Client{Timeout: 50 * time.Millisecond})
	_, _, err := r.ReadSourceFile(context.Background(), leaderWorkspace(1), ".utena/config.json")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLeaderSourceReader_RequiresLeaderWorkspaceID(t *testing.T) {
	_, _, err := NewLeaderSourceReader("http://127.0.0.1:1", nil).ReadSourceFile(context.Background(), Workspace{Name: "repo"}, ".utena/config.json")
	assert.ErrorIs(t, err, ErrNoLeaderWorkspace)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLeaderSourceReader_RejectsTraversal(t *testing.T) {
	_, _, err := NewLeaderSourceReader("http://127.0.0.1:1", nil).ReadSourceFile(context.Background(), leaderWorkspace(1), "../x")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}

func TestLeaderSourceReader_RoundTripsThroughFileEndpoints(t *testing.T) {
	handler, ws, _ := setupFilesRouter(t)
	mux := http.NewServeMux()
	mux.Handle("/workspaces/", http.StripPrefix("/workspaces", handler))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	require.NoError(t, os.WriteFile(filepath.Join(ws.Path, ".env"), []byte("A=1\n"), 0o600))

	r := NewLeaderSourceReader(srv.URL, nil)
	data, mode, err := r.ReadSourceFile(context.Background(), leaderWorkspace(ws.ID), ".env")
	require.NoError(t, err)
	assert.Equal(t, "A=1\n", string(data))
	assert.Equal(t, fs.FileMode(0o600), mode)

	_, _, err = r.ReadSourceFile(context.Background(), leaderWorkspace(ws.ID), ".utena/config.json")
	assert.ErrorIs(t, err, fs.ErrNotExist)

	_, _, err = r.ReadSourceFile(context.Background(), leaderWorkspace(ws.ID+100), ".env")
	require.Error(t, err)
	assert.False(t, errors.Is(err, fs.ErrNotExist))
}
