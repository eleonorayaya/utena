package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpFake struct {
	*FakeDriver
	hits atomic.Int32
}

func (h *httpFake) Name() string { return "fake" }
func (h *httpFake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.hits.Add(1) })
}

func newTestServer(t *testing.T) (*httptest.Server, *Module, *httpFake) {
	t.Helper()
	driver := &httpFake{FakeDriver: NewFakeDriver()}
	m := NewModule(driver, &FakeSessions{}, testdb.New(t, &BrowserWindow{}, &BrowserState{}))
	srv := httptest.NewServer(m.Routes())
	t.Cleanup(srv.Close)
	return srv, m, driver
}

func TestRouter_OpenReturns503WhenDisconnected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	_, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{SessionID: 1, URL: prURL})
	assert.ErrorIs(t, err, ErrUnavailable)
}

func TestRouter_OpenSucceedsWhenConnected(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})

	res, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{SessionID: 1, URL: prURL})
	require.NoError(t, err)
	assert.True(t, res.Created)
}

func TestRouter_OpenRejectsRelativeURL(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	_, err := RequestOpen(context.Background(), srv.Client(), srv.URL, OpenRequest{URL: "/relative"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrUnavailable)
}

func TestRouter_MountsDriverHandler(t *testing.T) {
	srv, _, driver := newTestServer(t)
	res, err := srv.Client().Get(srv.URL + "/fake")
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, int32(1), driver.hits.Load())
}

func TestRouter_Status(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	res, err := srv.Client().Get(srv.URL + "/status")
	require.NoError(t, err)
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}

func TestRouter_OpenRejectsCrossOriginRequests(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/open", strings.NewReader(`{"session_id":1,"url":"`+prURL+`"}`))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	res.Body.Close()

	assert.Equal(t, http.StatusForbidden, res.StatusCode)
	windows, _ := driver.ListWindows(context.Background())
	assert.Empty(t, windows)
}

func TestRouter_StatusRejectsCrossOriginRequests(t *testing.T) {
	srv, _, _ := newTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/status", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "null")
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, http.StatusForbidden, res.StatusCode)
}

func TestRouter_DriverHandlerAcceptsExtensionOrigin(t *testing.T) {
	srv, _, driver := newTestServer(t)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/fake", nil)
	require.NoError(t, err)
	req.Header.Set("Origin", "chrome-extension://abc")
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	res.Body.Close()
	assert.Equal(t, int32(1), driver.hits.Load())
}
