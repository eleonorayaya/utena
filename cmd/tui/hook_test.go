package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func initHookTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"-c", "user.email=t@t", "-c", "user.name=T", "commit", "--allow-empty", "-m", "init"},
		{"checkout", "-b", "eqt/hooked"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, string(out))
	}
	return dir
}

func TestReportCheckout_SendsPathAndBranch(t *testing.T) {
	dir := initHookTestRepo(t)
	t.Chdir(dir)

	var got map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/sessions/branch-hook", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	reportCheckout(context.Background(), server.URL, []string{"a", "b", "1"})

	canonical, err := filepath.EvalSymlinks(dir)
	require.NoError(t, err)
	require.Equal(t, canonical, got["path"])
	require.Equal(t, "eqt/hooked", got["branch"])
}

func TestReportCheckout_IgnoresFileCheckout(t *testing.T) {
	t.Chdir(initHookTestRepo(t))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	reportCheckout(context.Background(), server.URL, []string{"a", "b", "0"})
	reportCheckout(context.Background(), server.URL, nil)

	require.False(t, called)
}

func TestReportCheckout_UnreachableDaemon(t *testing.T) {
	t.Chdir(initHookTestRepo(t))
	start := time.Now()

	reportCheckout(context.Background(), "http://127.0.0.1:1", []string{"a", "b", "1"})

	require.Less(t, time.Since(start), 2*time.Second)
}
