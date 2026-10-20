package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoderStatus_NotConfiguredByDefault(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/coder/status", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"configured":false,"connections":[]}`, w.Body.String())
}

func TestCreateCoderSession_RejectedWithoutTemplate(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)
	body := fmt.Sprintf(`{"name":"remote","backend":"coder","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(body))))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.True(t, strings.Contains(w.Body.String(), "coder.template"), w.Body.String())
}

func TestCreateSession_RejectsUnknownBackend(t *testing.T) {
	_, router, _, ws1ID, _ := setupTestRouter(t)
	body := fmt.Sprintf(`{"name":"remote","backend":"cloud","workspaces":[{"workspace_id":%d,"base_branch":"main"}]}`, ws1ID)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(body))))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCoderRefresh_UnknownSession(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/coder/sessions/99/refresh", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
