package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	body   []byte
}

func recordingServer(t *testing.T, status int, response string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.method = r.Method
		rec.path = r.URL.Path
		rec.query = r.URL.Query()
		rec.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func TestFetchSessions_SkipsDeleted(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{"sessions":[]}`)
	newClient(srv.URL).fetchSessions()()
	assert.Equal(t, "/sessions", rec.path)
	assert.Equal(t, []string{"creating,active,broken,pending,inactive,archived,completed"}, rec.query["status"])
}

func TestFetchBundles_DecodesCatalog(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{"bundles":[{"name":"go","plugins":["go@m"]}]}`)
	msg := newClient(srv.URL).fetchBundles()()
	assert.Equal(t, BundlesLoadedMsg{Bundles: []bundles.Entry{{Name: "go", Plugins: []string{"go@m"}}}}, msg)
	assert.Equal(t, "/bundles", rec.path)
}

func TestFetchBundles_FailureIsQuiet(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusNotFound, `404 page not found`)
	msg := newClient(srv.URL).fetchBundles()()
	loaded, ok := msg.(BundlesLoadedMsg)
	require.True(t, ok, "got %T", msg)
	assert.EqualError(t, loaded.Err, "fetch bundles: unexpected status 404")
}

func TestFetchBundles_ServerErrorIsQuiet(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusInternalServerError, `{"error":"broken catalog"}`)
	msg := newClient(srv.URL).fetchBundles()()
	loaded, ok := msg.(BundlesLoadedMsg)
	require.True(t, ok, "got %T", msg)
	assert.EqualError(t, loaded.Err, "broken catalog")
}

func TestFetchDefaultBundles_SendsWorkspaceIDs(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{"bundles":["go"]}`)
	msg := newClient(srv.URL).fetchDefaultBundles([]uint{1, 2})()
	assert.Equal(t, DefaultBundlesLoadedMsg{WorkspaceIDs: []uint{1, 2}, Bundles: []string{"go"}}, msg)
	assert.Equal(t, "/sessions/default-bundles", rec.path)
	assert.Equal(t, []string{"1", "2"}, rec.query["workspace_id"])
}

func TestFetchDefaultBundles_FailureIsQuiet(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusNotFound, `404 page not found`)
	msg := newClient(srv.URL).fetchDefaultBundles([]uint{4})()
	loaded, ok := msg.(DefaultBundlesLoadedMsg)
	require.True(t, ok, "got %T", msg)
	assert.Equal(t, []uint{4}, loaded.WorkspaceIDs)
	assert.EqualError(t, loaded.Err, "fetch default bundles: unexpected status 404")
}

func TestSetSessionBundles_PutsBundles(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{}`)
	msg := newClient(srv.URL).setSessionBundles(7, []string{"go"})()
	assert.Equal(t, SessionBundlesSetMsg{SessionID: 7}, msg)
	assert.Equal(t, http.MethodPut, rec.method)
	assert.Equal(t, "/sessions/7/bundles", rec.path)
	assert.JSONEq(t, `{"bundles":["go"]}`, string(rec.body))
}

func TestSetSessionBundles_NilSendsEmptyArray(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{}`)
	newClient(srv.URL).setSessionBundles(7, nil)()
	assert.JSONEq(t, `{"bundles":[]}`, string(rec.body))
}

func TestSetSessionBundles_ErrorStatus(t *testing.T) {
	srv, _ := recordingServer(t, http.StatusBadRequest, `{"error":"nope"}`)
	msg := newClient(srv.URL).setSessionBundles(7, []string{"go"})()
	errMsg, ok := msg.(ErrMsg)
	require.True(t, ok, "got %T", msg)
	assert.Equal(t, "nope", errMsg.Err.Error())
}

func createSessionBody(t *testing.T, selected []string) map[string]any {
	t.Helper()
	srv, rec := recordingServer(t, http.StatusAccepted, `{"id":3}`)
	newClient(srv.URL).createSession("s", []SessionWorkspaceSpec{{WorkspaceID: 1}}, selected)()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.body, &body))
	return body
}

func TestCreateSession_OmitsBundlesWhenNil(t *testing.T) {
	body := createSessionBody(t, nil)
	_, present := body["bundles"]
	assert.False(t, present)
}

func TestCreateSession_SendsEmptyBundles(t *testing.T) {
	body := createSessionBody(t, []string{})
	assert.Equal(t, []any{}, body["bundles"])
}

func TestCreateSession_SendsBundles(t *testing.T) {
	body := createSessionBody(t, []string{"go"})
	assert.Equal(t, []any{"go"}, body["bundles"])
}

func TestSessionsProvider_SessionBundlesSetRefetchesSessions(t *testing.T) {
	srv, rec := recordingServer(t, http.StatusOK, `{"sessions":[]}`)
	p := newSessionsProvider(newClient(srv.URL))
	_, cmd := p.Update(SessionBundlesSetMsg{SessionID: 7})
	require.NotNil(t, cmd)
	uitest.Drain(cmd)
	assert.Equal(t, "/sessions", rec.path)
}
