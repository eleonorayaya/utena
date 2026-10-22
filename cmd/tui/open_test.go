package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
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
	err := openURL(context.Background(), io.Discard, srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, browser.OpenRequest{SessionID: 7, URL: "https://x.example.com"}, got)
	assert.Empty(t, fellBack)
}

func TestOpenURL_PendingPrintsNotice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(browser.OpenResult{Pending: true})
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := openURL(context.Background(), &out, srv.URL, 7, "https://x.example.com", func(string) error { t.Fatal("fell back"); return nil })
	require.NoError(t, err)
	assert.Equal(t, "utena: https://x.example.com is waiting in session 7's browser window\n", out.String())
}

func TestOpenURL_FallsBackOn503(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	var fellBack []string
	err := openURL(context.Background(), io.Discard, srv.URL, 7, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example.com"}, fellBack)
}

func TestOpenURL_FallsBackWhenDaemonDown(t *testing.T) {
	var fellBack []string
	err := openURL(context.Background(), io.Discard, "http://127.0.0.1:1", 0, "https://x.example.com", func(u string) error { fellBack = append(fellBack, u); return nil })
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

func TestFallbackEnvDropsBrowserAndSetsGuard(t *testing.T) {
	got := fallbackEnv([]string{"HOME=/h", "BROWSER=/x/utena-open", "UTENA_OPEN_FALLBACK=old", "PATH=/bin"}, nil, 42)
	assert.Equal(t, []string{"HOME=/h", "PATH=/bin", "UTENA_OPEN_FALLBACK=42"}, got)
}

func TestFallbackEnvFillsMissingDesktopVars(t *testing.T) {
	got := fallbackEnv(
		[]string{"HOME=/h", "DISPLAY=:9"},
		[]string{"DISPLAY=:0", "WAYLAND_DISPLAY=wayland-0", "XDG_CURRENT_DESKTOP=KDE", "PATH=/other"},
		42,
	)
	assert.Equal(t, []string{"HOME=/h", "DISPLAY=:9", "WAYLAND_DISPLAY=wayland-0", "XDG_CURRENT_DESKTOP=KDE", "UTENA_OPEN_FALLBACK=42"}, got)
}

func TestSystemOpenRefusesToRecurse(t *testing.T) {
	t.Setenv(fallbackEnvVar, strconv.Itoa(os.Getppid()))
	err := systemOpen("https://x.example.com")
	require.Error(t, err)
}

func TestInsideFallback(t *testing.T) {
	parents := map[int]int{50: 40, 40: 30, 30: 1}
	parent := func(p int) int { return parents[p] }

	assert.True(t, insideFallback("40", 50, parent), "guard pid is an ancestor: recursion")
	assert.True(t, insideFallback("30", 50, parent))
	assert.False(t, insideFallback("99", 50, parent), "guard pid gone: inherited by a later process")
	assert.False(t, insideFallback("", 50, parent))
	assert.False(t, insideFallback("1", 50, parent))
	assert.False(t, insideFallback("junk", 50, parent))
}
