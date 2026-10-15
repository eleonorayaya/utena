package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type httpFake struct {
	*FakeDriver
	hits int
}

func (h *httpFake) Name() string { return "fake" }
func (h *httpFake) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.hits++ })
}

func newTestServer(t *testing.T) (*httptest.Server, *Module, *httpFake) {
	t.Helper()
	driver := &httpFake{FakeDriver: NewFakeDriver()}
	m := NewModule(driver, &FakeSessions{}, testdb.New(t, &BrowserWindow{}))
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
	assert.Equal(t, 1, driver.hits)
}

func TestRouter_Status(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	res, err := srv.Client().Get(srv.URL + "/status")
	require.NoError(t, err)
	defer res.Body.Close()
	assert.Equal(t, http.StatusOK, res.StatusCode)
}
