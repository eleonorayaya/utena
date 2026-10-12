package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func doJSON(t *testing.T, router *SessionRouter, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.Routes().ServeHTTP(w, req)
	return w
}

func TestSessionRouter_AddBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive, LastUsedAt: time.Now()}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)

	w := doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "review", "url": "https://github.com/o/r/pull/1"})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.IsBlocked)
	require.Len(t, resp.Blockers, 1)
	require.Equal(t, BlockerKindReview, resp.Blockers[0].Kind)
	require.Equal(t, StatusActive, resp.Status)
}

func TestSessionRouter_AddBlocker_InvalidKind(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)

	w := doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "email"})
	require.Equal(t, http.StatusBadRequest, w.Code)

	w = doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{})
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSessionRouter_AddBlocker_UnknownOrDeletedSession(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	w := doJSON(t, router, "POST", "/9999/blockers", map[string]string{"kind": "other"})
	require.Equal(t, http.StatusNotFound, w.Code)

	sess := &Session{Name: "gone", Status: StatusDeleted}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	w = doJSON(t, router, "POST", fmt.Sprintf("/%d/blockers", sess.ID), map[string]string{"kind": "other"})
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestSessionRouter_ResolveBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusInactive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindSlack}
	require.NoError(t, bs.Create(b))

	path := fmt.Sprintf("/%d/blockers/%d/resolve", sess.ID, b.ID)
	w := doJSON(t, router, "PUT", path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SessionResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.False(t, resp.IsBlocked)
	require.Equal(t, StatusInactive, resp.Status)

	first, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.NotNil(t, first.ResolvedAt)

	w = doJSON(t, router, "PUT", path, nil)
	require.Equal(t, http.StatusOK, w.Code)
	again, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.True(t, first.ResolvedAt.Equal(*again.ResolvedAt))
}

func TestSessionRouter_BlockerOwnership(t *testing.T) {
	router, sessionStore, _, _, ws1ID, ws2ID := setupSessionRouter(t)
	swStore := NewSessionWorktreeStore(sessionStore.db)
	a := &Session{Name: "a", Status: StatusActive}
	other := &Session{Name: "b", Status: StatusActive}
	addTestSession(t, sessionStore, swStore, a, ws1ID)
	addTestSession(t, sessionStore, swStore, other, ws2ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: a.ID, Kind: BlockerKindOther}
	require.NoError(t, bs.Create(b))

	w := doJSON(t, router, "PUT", fmt.Sprintf("/%d/blockers/%d/resolve", other.ID, b.ID), nil)
	require.Equal(t, http.StatusNotFound, w.Code)
	w = doJSON(t, router, "DELETE", fmt.Sprintf("/%d/blockers/%d", other.ID, b.ID), nil)
	require.Equal(t, http.StatusNotFound, w.Code)

	still, err := bs.GetByID(b.ID)
	require.NoError(t, err)
	require.Nil(t, still.ResolvedAt)
}

func TestSessionRouter_DeleteBlocker(t *testing.T) {
	router, sessionStore, _, _, ws1ID, _ := setupSessionRouter(t)
	sess := &Session{Name: "s1", Status: StatusActive}
	addTestSession(t, sessionStore, NewSessionWorktreeStore(sessionStore.db), sess, ws1ID)
	bs := NewSessionBlockerStore(sessionStore.db)
	b := &SessionBlocker{SessionID: sess.ID, Kind: BlockerKindOther}
	require.NoError(t, bs.Create(b))

	w := doJSON(t, router, "DELETE", fmt.Sprintf("/%d/blockers/%d", sess.ID, b.ID), nil)
	require.Equal(t, http.StatusNoContent, w.Code)
	_, err := bs.GetByID(b.ID)
	require.ErrorIs(t, err, ErrBlockerNotFound)
}
