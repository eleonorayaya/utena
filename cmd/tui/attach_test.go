package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTuiosAttachArgs(t *testing.T) {
	assert.Equal(t, []string{"attach"}, tuiosAttachArgs(attachTarget{}))
	assert.Equal(t, []string{"attach", "work"}, tuiosAttachArgs(attachTarget{SessionName: "work"}))
	assert.Equal(t, []string{"attach", "--host", "ws-1", "work"}, tuiosAttachArgs(attachTarget{SessionName: "work", Host: "ws-1"}))
	assert.Equal(t, []string{"attach"}, tuiosAttachArgs(attachTarget{Host: "ws-1"}))
}

func serverPort(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u.Port()
}

func TestNextActivation_DecodesHost(t *testing.T) {
	bodies := []string{`{"session_name":"work"}`, `{"session_name":"remote","host":"ws-1"}`}
	i := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/mux/activations/next", r.URL.Path)
		_, _ = io.WriteString(w, bodies[i])
		i++
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	got, err := nextActivation(context.Background(), port)
	require.NoError(t, err)
	assert.Equal(t, attachTarget{SessionName: "work"}, got)

	got, err = nextActivation(context.Background(), port)
	require.NoError(t, err)
	assert.Equal(t, attachTarget{SessionName: "remote", Host: "ws-1"}, got)
}

func TestNextActivation_NoContentIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	got, err := nextActivation(context.Background(), serverPort(t, srv))
	require.NoError(t, err)
	assert.Equal(t, attachTarget{}, got)
}

func TestPostClientHook_LocalBodyUnchanged(t *testing.T) {
	var bodies []string
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, strings.TrimSpace(string(b)))
		paths = append(paths, r.URL.Path)
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	postClientHook(port, "client-session-changed", attachTarget{SessionName: "work"})
	postClientHook(port, "client-detached", attachTarget{SessionName: "remote", Host: "ws-1"})

	assert.Equal(t, []string{"/mux/hooks/client-session-changed", "/mux/hooks/client-detached"}, paths)
	assert.Equal(t, []string{`{"session_name":"work"}`, `{"session_name":"remote","host":"ws-1"}`}, bodies)
}
