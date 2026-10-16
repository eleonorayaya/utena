package provider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func browserServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func sampleView() browser.SessionsView {
	return browser.SessionsView{
		Connected: true,
		Outdated:  true,
		Sessions: map[uint]browser.SessionBrowser{
			7: {State: "open", TabCount: 1, Windows: []browser.WindowView{{ID: "w1", State: "open", Focused: true, Tabs: []browser.TabView{{ID: "t1", Title: "Docs", URL: "https://docs.example.com", Active: true}}}}},
		},
	}
}

func TestFetchBrowserState_DecodesView(t *testing.T) {
	var path string
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_ = json.NewEncoder(w).Encode(sampleView())
	})
	msg := newClient(srv.URL).fetchBrowserState()()
	assert.Equal(t, "GET /browser/sessions", path)
	assert.Equal(t, browserStateLoadedMsg{state: sampleView()}, msg)
}

func TestFetchBrowserState_NotFoundIsEmpty(t *testing.T) {
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	msg := newClient(srv.URL).fetchBrowserState()()
	assert.Equal(t, browserStateLoadedMsg{state: browser.SessionsView{Sessions: map[uint]browser.SessionBrowser{}}}, msg)
}

func TestFetchBrowserState_FailureEmitsNothing(t *testing.T) {
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	assert.Nil(t, newClient(srv.URL).fetchBrowserState()())
}

func TestFocusTab_PostsRequest(t *testing.T) {
	var got browser.FocusRequest
	var path string
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.Method + " " + r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(browser.FocusResult{WindowID: "w1", TabID: "t1", Focused: true})
	})
	msg := newClient(srv.URL).focusTab(7, "w1", "t1")()
	assert.Equal(t, "POST /browser/focus", path)
	assert.Equal(t, browser.FocusRequest{SessionID: 7, WindowID: "w1", TabID: "t1"}, got)
	assert.Equal(t, browserFocusedMsg{}, msg)
}

func TestFocusTab_ActivatedSwitchesSession(t *testing.T) {
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(browser.FocusResult{WindowID: "w1", TabID: "t1", Focused: true, Activated: true})
	})
	assert.Equal(t, SessionSwitchedMsg{}, newClient(srv.URL).focusTab(7, "w1", "t1")())
}

func TestFocusTab_ErrorReturnsErrMsg(t *testing.T) {
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "browser unavailable"})
	})
	msg, ok := newClient(srv.URL).focusTab(7, "w1", "t1")().(browserFocusFailedMsg)
	require.True(t, ok)
	assert.ErrorIs(t, msg.err, browser.ErrUnavailable)
}

func TestBrowserProvider_FailedFocusRefetchesAndReportsError(t *testing.T) {
	calls := 0
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(sampleView())
	})
	p := newBrowserProvider(newClient(srv.URL))
	_, cmd := p.Update(browserFocusFailedMsg{err: errors.New("tab gone")})
	require.NotNil(t, cmd)
	batch, ok := cmd().(tea.BatchMsg)
	require.True(t, ok)
	var msgs []tea.Msg
	for _, c := range batch {
		msgs = append(msgs, c())
	}
	assert.ElementsMatch(t, []tea.Msg{ErrMsg{errors.New("tab gone")}, browserStateLoadedMsg{state: sampleView()}}, msgs)
	assert.Equal(t, 1, calls)
}

func TestFocusTab_IntentCmd(t *testing.T) {
	assert.Equal(t, FocusTabIntentMsg{SessionID: 7, WindowID: "w1", TabID: "t1"}, FocusTab(7, "w1", "t1")())
}

func TestBrowserProvider_EmitsLoadedStateAndKeepsIt(t *testing.T) {
	p := newBrowserProvider(newClient("http://127.0.0.1:0"))
	p, cmd := p.Update(browserStateLoadedMsg{state: sampleView()})
	require.NotNil(t, cmd)
	assert.Equal(t, BrowserStateUpdatedMsg{State: sampleView()}, cmd())

	p, cmd = p.Update(RequestBrowserState()())
	require.NotNil(t, cmd)
	assert.Equal(t, BrowserStateUpdatedMsg{State: sampleView()}, cmd())
}

func TestBrowserProvider_FetchesOnSessionsLoadedAndAfterFocus(t *testing.T) {
	calls := 0
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(sampleView())
	})
	p := newBrowserProvider(newClient(srv.URL))
	for _, msg := range []tea.Msg{sessionsLoadedMsg{}, browserFocusedMsg{}} {
		var cmd tea.Cmd
		p, cmd = p.Update(msg)
		require.NotNil(t, cmd)
		assert.Equal(t, browserStateLoadedMsg{state: sampleView()}, cmd())
	}
	assert.Equal(t, 2, calls)
}

func TestBrowserProvider_FocusIntentPostsFocus(t *testing.T) {
	var path string
	srv := browserServer(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_ = json.NewEncoder(w).Encode(browser.FocusResult{Activated: true})
	})
	p := newBrowserProvider(newClient(srv.URL))
	_, cmd := p.Update(FocusTabIntentMsg{SessionID: 7, WindowID: "w1", TabID: "t1"})
	require.NotNil(t, cmd)
	assert.Equal(t, SessionSwitchedMsg{}, cmd())
	assert.Equal(t, "/browser/focus", path)
}
