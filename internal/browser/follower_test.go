package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLeaderSessions struct {
	ids map[uint]*uint
	err error
}

func (f fakeLeaderSessions) LeaderSessionID(_ context.Context, id uint) (*uint, error) {
	if f.err != nil {
		return nil, f.err
	}
	leaderID, ok := f.ids[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return leaderID, nil
}

type fakeLeader struct {
	mu       sync.Mutex
	requests []OpenRequest
	status   int
	result   OpenResult
}

func (l *fakeLeader) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/browser/open", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Empty(t, r.Header.Values("Origin"))
		var req OpenRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		l.mu.Lock()
		l.requests = append(l.requests, req)
		l.mu.Unlock()
		if l.status != 0 && l.status != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(l.status)
			_, _ = w.Write([]byte(`{"error":"leader says no"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(l.result)
	})
}

func uintPtr(v uint) *uint { return &v }

func setupFollower(t *testing.T, leader *fakeLeader, sessions LeaderSessions) (http.Handler, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(leader.handler(t))
	t.Cleanup(srv.Close)
	return NewFollowerModule(srv.URL+"/", nil, sessions).Routes(), srv
}

func postOpen(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/open", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestFollower_OpenForwardsWithLeaderSessionID(t *testing.T) {
	leader := &fakeLeader{result: OpenResult{WindowID: "w1", TabID: "t1", Focused: true}}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{ids: map[uint]*uint{3: uintPtr(41)}})

	w := postOpen(t, h, `{"session_id":3,"url":"https://example.com/a","activate":true}`)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res OpenResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, OpenResult{WindowID: "w1", TabID: "t1", Focused: true}, res)
	require.Len(t, leader.requests, 1)
	assert.Equal(t, OpenRequest{SessionID: 41, URL: "https://example.com/a", Activate: true}, leader.requests[0])
}

func TestFollower_OpenWithoutSessionForwardsSessionZero(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{})

	w := postOpen(t, h, `{"url":"https://example.com"}`)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, leader.requests, 1)
	assert.Equal(t, uint(0), leader.requests[0].SessionID)
}

func TestFollower_OpenUnknownSessionForwardsSessionZero(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{ids: map[uint]*uint{}})

	w := postOpen(t, h, `{"session_id":9,"url":"https://example.com"}`)

	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, leader.requests, 1)
	assert.Equal(t, uint(0), leader.requests[0].SessionID)
}

func TestFollower_OpenSessionWithoutLeaderIDIsRejected(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{ids: map[uint]*uint{3: nil}})

	w := postOpen(t, h, `{"session_id":3,"url":"https://example.com"}`)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "leader session")
	assert.Empty(t, leader.requests)
}

func TestFollower_OpenLookupErrorIsReturned(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{err: errors.New("db down")})

	w := postOpen(t, h, `{"session_id":3,"url":"https://example.com"}`)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Empty(t, leader.requests)
}

func TestFollower_OpenRejectsRelativeURL(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{})

	w := postOpen(t, h, `{"url":"example.com"}`)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Empty(t, leader.requests)
}

func TestFollower_OpenLeaderErrorIsUnavailable(t *testing.T) {
	leader := &fakeLeader{status: http.StatusInternalServerError}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{})

	w := postOpen(t, h, `{"url":"https://example.com"}`)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "leader says no")
}

func TestFollower_OpenLeaderUnavailableIsUnavailable(t *testing.T) {
	leader := &fakeLeader{status: http.StatusServiceUnavailable}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{})

	w := postOpen(t, h, `{"url":"https://example.com"}`)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestFollower_OpenLeaderUnreachableIsUnavailable(t *testing.T) {
	leader := &fakeLeader{}
	h, srv := setupFollower(t, leader, fakeLeaderSessions{})
	srv.Close()

	w := postOpen(t, h, `{"url":"https://example.com"}`)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "open on leader failed")
}

func TestFollower_OpenThroughRequestOpenClient(t *testing.T) {
	leader := &fakeLeader{result: OpenResult{Pending: true}}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{ids: map[uint]*uint{2: uintPtr(20)}})
	follower := httptest.NewServer(h)
	defer follower.Close()

	res, err := RequestOpen(context.Background(), http.DefaultClient, follower.URL, OpenRequest{SessionID: 2, URL: "https://example.com"})
	require.NoError(t, err)
	assert.True(t, res.Pending)
	assert.Equal(t, uint(20), leader.requests[0].SessionID)
}

func TestFollower_OtherRoutesAreInert(t *testing.T) {
	h, _ := setupFollower(t, &fakeLeader{}, fakeLeaderSessions{})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"connected":false,"outdated":false}`, w.Body.String())

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sessions", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var view SessionsView
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &view))
	assert.False(t, view.Connected)
	assert.NotNil(t, view.Sessions)
	assert.Empty(t, view.Sessions)

	req := httptest.NewRequest(http.MethodPost, "/focus", strings.NewReader(`{"session_id":1,"window_id":"w","tab_id":"t"}`))
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/chrome", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestFollower_RejectsCrossOrigin(t *testing.T) {
	leader := &fakeLeader{}
	h, _ := setupFollower(t, leader, fakeLeaderSessions{})

	req := httptest.NewRequest(http.MethodPost, "/open", strings.NewReader(`{"url":"https://example.com"}`))
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, leader.requests)
}

func TestFollower_RegistersBrowserModels(t *testing.T) {
	assert.Len(t, NewFollowerModule("http://127.0.0.1:1", nil, fakeLeaderSessions{}).Models(), 2)
}
