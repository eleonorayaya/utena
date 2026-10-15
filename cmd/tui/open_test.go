package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenURL_SendsSessionAndSkipsFallback(t *testing.T) {
	var got browser.OpenRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/browser/open", r.URL.Path)
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(browser.OpenResult{WindowID: "1"})
	}))
	defer srv.Close()

	var fellBack []string
	err := openURL(context.Background(), srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, browser.OpenRequest{SessionID: 7, URL: "https://x.example.com"}, got)
	assert.Empty(t, fellBack)
}

func TestOpenURL_FallsBackOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var fellBack []string
	err := openURL(context.Background(), srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example.com"}, fellBack)
}

func TestOpenURL_FallsBackWhenDaemonDown(t *testing.T) {
	var fellBack []string
	err := openURL(context.Background(), "http://127.0.0.1:1", 0, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example.com"}, fellBack)
}

func TestSessionIDFromEnv(t *testing.T) {
	t.Setenv("UTENA_SESSION_ID", "12")
	assert.Equal(t, uint(12), sessionIDFromEnv())
	t.Setenv("UTENA_SESSION_ID", "junk")
	assert.Equal(t, uint(0), sessionIDFromEnv())
	t.Setenv("UTENA_SESSION_ID", "")
	assert.Equal(t, uint(0), sessionIDFromEnv())
}

func TestRewriteArgsForBrowserAlias(t *testing.T) {
	assert.Equal(t, []string{"/x/utena-open", "open", "https://a"}, rewriteArgs([]string{"/x/utena-open", "https://a"}))
	assert.Equal(t, []string{"/x/utena", "top"}, rewriteArgs([]string{"/x/utena", "top"}))
}
