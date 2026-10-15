package browser

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/wm"
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
	m := NewModule(driver, &FakeSessions{}, wm.None{}, Layout{BrowserWorkspace: "2", ParkWorkspace: "P"}, eventbus.NewEventBus(), testdb.New(t, &BrowserWindow{}, &BrowserState{}))
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

func TestRouter_StatusReportsConnectedAndOutdated(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	driver.SetOutdated(true)

	res, err := srv.Client().Get(srv.URL + "/status")
	require.NoError(t, err)
	defer res.Body.Close()
	var body map[string]any
	require.NoError(t, json.NewDecoder(res.Body).Decode(&body))
	assert.Equal(t, map[string]any{"connected": true, "outdated": true}, body)
}

func TestRouter_SessionsJSONShape(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	w := driver.AddWindow("https://a.example.com")
	driver.SetFocused(w.ID)
	driver.SetTitle(w.ID, "A")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 7, State: WindowOpen, DriverWindowID: w.ID}))
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 7, State: WindowSnapshotted, Tabs: []SavedTab{{URL: "https://s.example.com", Title: "S"}}}))

	res, err := srv.Client().Get(srv.URL + "/sessions")
	require.NoError(t, err)
	defer res.Body.Close()
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.JSONEq(t, `{
		"connected": true,
		"outdated": false,
		"sessions": {
			"7": {
				"state": "open",
				"tab_count": 2,
				"windows": [
					{"id": "`+w.ID+`", "state": "open", "focused": true, "tabs": [{"id": "`+w.Tabs[0].ID+`", "title": "A", "url": "https://a.example.com", "active": true}]},
					{"id": "", "state": "snapshotted", "focused": false, "tabs": [{"id": "", "title": "S", "url": "https://s.example.com", "active": false}]}
				]
			}
		}
	}`, string(raw))

	var view SessionsView
	require.NoError(t, json.Unmarshal(raw, &view))
	assert.Equal(t, 2, view.Sessions[7].TabCount)
}

func TestRouter_SessionsEmpty(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res, err := srv.Client().Get(srv.URL + "/sessions")
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"connected": false, "outdated": false, "sessions": {}}`, string(raw))
}

func postFocus(t *testing.T, srv *httptest.Server, body string) *http.Response {
	t.Helper()
	res, err := srv.Client().Post(srv.URL+"/focus", "application/json", strings.NewReader(body))
	require.NoError(t, err)
	t.Cleanup(func() { res.Body.Close() })
	return res
}

func TestRouter_FocusSucceeds(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	w := driver.AddWindow("https://a.example.com", "https://b.example.com")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: w.ID}))

	res := postFocus(t, srv, `{"session_id":3,"window_id":"`+w.ID+`","tab_id":"`+w.Tabs[0].ID+`"}`)
	require.Equal(t, http.StatusOK, res.StatusCode)
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"window_id":"`+w.ID+`","tab_id":"`+w.Tabs[0].ID+`","focused":true,"activated":true}`, string(raw))
	assert.Equal(t, []string{w.ID}, driver.Focused())
}

func TestRouter_FocusReturns503WhenDisconnected(t *testing.T) {
	srv, _, _ := newTestServer(t)
	res := postFocus(t, srv, `{"session_id":3,"window_id":"w1","tab_id":"t2"}`)
	assert.Equal(t, http.StatusServiceUnavailable, res.StatusCode)
}

func TestRouter_FocusRejectsWindowNotOwnedBySession(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	w := driver.AddWindow("https://a.example.com")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 4, State: WindowOpen, DriverWindowID: w.ID}))

	res := postFocus(t, srv, `{"session_id":3,"window_id":"`+w.ID+`","tab_id":"`+w.Tabs[0].ID+`"}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.Empty(t, driver.Focused())
}

func TestRouter_FocusRejectsIncompleteRequest(t *testing.T) {
	srv, m, _ := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	for _, body := range []string{
		`{"window_id":"w1","tab_id":"t2"}`,
		`{"session_id":3,"tab_id":"t2"}`,
		`{"session_id":3,"window_id":"w1"}`,
	} {
		res := postFocus(t, srv, body)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode, body)
	}
}

func TestRouter_NewRoutesRejectCrossOriginRequests(t *testing.T) {
	srv, m, driver := newTestServer(t)
	m.Service.handleEvent(context.Background(), Event{Type: EventConnected})
	w := driver.AddWindow("https://a.example.com", "https://b.example.com")
	require.NoError(t, m.Service.store.Add(&BrowserWindow{SessionID: 3, State: WindowOpen, DriverWindowID: w.ID}))

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodGet, "/sessions", ""},
		{http.MethodPost, "/focus", `{"session_id":3,"window_id":"` + w.ID + `","tab_id":"` + w.Tabs[0].ID + `"}`},
	} {
		req, err := http.NewRequest(tc.method, srv.URL+tc.path, strings.NewReader(tc.body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://evil.example")
		res, err := srv.Client().Do(req)
		require.NoError(t, err)
		res.Body.Close()
		assert.Equal(t, http.StatusForbidden, res.StatusCode, tc.path)
	}
	assert.Empty(t, driver.Focused())
	assert.Equal(t, w.Tabs[1].ID, activeTabID(driver.FakeDriver, w.ID))
}

func activeTabID(d *FakeDriver, windowID string) string {
	w, _ := d.Window(windowID)
	tab, _ := w.ActiveTab()
	return tab.ID
}
