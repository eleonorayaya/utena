package provider

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
)

func openServer(t *testing.T, res browser.OpenResult) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(res)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestOpenURL_SwitchedSessionQuits(t *testing.T) {
	srv := openServer(t, browser.OpenResult{WindowID: "1", Activated: true})
	msg := newClient(srv.URL).openURL(1, "https://x.example.com")()
	assert.Equal(t, SessionSwitchedMsg{}, msg)
}

func TestOpenURL_NoSwitchStaysOpen(t *testing.T) {
	srv := openServer(t, browser.OpenResult{WindowID: "1"})
	msg := newClient(srv.URL).openURL(1, "https://x.example.com")()
	assert.Nil(t, msg)
}
