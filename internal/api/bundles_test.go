package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBundlesRouteMounted(t *testing.T) {
	_, router, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bundles", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[]}`, w.Body.String())
}
