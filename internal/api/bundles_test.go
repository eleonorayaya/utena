package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/mux"
	"github.com/spf13/afero"
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

func TestBundlesRouteIncludesMcpServers(t *testing.T) {
	gormDB, err := db.OpenInMemorySQLite()
	require.NoError(t, err)

	cfg := Config{
		ConfigDir:    "/config",
		SessionsRoot: t.TempDir(),
		WM:           "none",
		Browser:      "chrome",
		Bundles: bundles.Catalog{
			"go":     {Plugins: []string{"go@m"}, McpServers: []string{"gopls"}},
			"notion": {McpServers: []string{"notion"}},
		},
		McpServers: bundles.Servers{
			"gopls":  json.RawMessage(`{"command":"gopls"}`),
			"notion": json.RawMessage(`{"type":"http","url":"https://mcp.notion.com/mcp"}`),
		},
	}
	bus := eventbus.NewEventBus()
	database := db.NewDB(gormDB)
	app := buildApp(gormDB, afero.NewMemMapFs(), cfg, mux.NewModule(mux.NewFakeDriver(), bus, database), git.NewGitModule(database, bus), bus)
	require.NoError(t, app.OnStart(context.Background()))

	w := httptest.NewRecorder()
	BuildServer(app, cfg).Handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bundles", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[{"name":"go","plugins":["go@m"],"mcp_servers":["gopls"]},{"name":"notion","plugins":[],"mcp_servers":["notion"]}]}`, w.Body.String())
}
