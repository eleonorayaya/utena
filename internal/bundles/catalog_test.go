package bundles

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCatalog() Catalog {
	return Catalog{
		"go":   {Plugins: []string{"go@m", "lsp@m"}},
		"docs": {Plugins: []string{"pdf@m", "lsp@m"}},
	}
}

func serverCatalog() Catalog {
	return Catalog{
		"go":     {Plugins: []string{"go@m"}, McpServers: []string{"gopls", "github"}},
		"docs":   {Plugins: []string{"pdf@m"}, McpServers: []string{"github"}},
		"notion": {McpServers: []string{"notion"}},
	}
}

func TestCatalog_Managed(t *testing.T) {
	plugins, servers := testCatalog().Managed()
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, plugins)
	assert.Empty(t, servers)

	plugins, servers = serverCatalog().Managed()
	assert.Equal(t, []string{"go@m", "pdf@m"}, plugins)
	assert.Equal(t, []string{"github", "gopls", "notion"}, servers)

	plugins, servers = Catalog(nil).Managed()
	assert.Empty(t, plugins)
	assert.Empty(t, servers)
}

func TestCatalog_Resolve(t *testing.T) {
	plugins, servers, unknown := testCatalog().Resolve([]string{"docs", "nope", "go"})
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, plugins)
	assert.Empty(t, servers)
	assert.Equal(t, []string{"nope"}, unknown)

	plugins, servers, unknown = Catalog(nil).Resolve([]string{"go"})
	assert.Empty(t, plugins)
	assert.Empty(t, servers)
	assert.Equal(t, []string{"go"}, unknown)
}

func TestCatalog_ResolveWithServers(t *testing.T) {
	plugins, servers, unknown := serverCatalog().Resolve([]string{"docs", "nope", "go", "notion"})
	assert.Equal(t, []string{"go@m", "pdf@m"}, plugins)
	assert.Equal(t, []string{"github", "gopls", "notion"}, servers)
	assert.Equal(t, []string{"nope"}, unknown)

	plugins, servers, unknown = serverCatalog().Resolve([]string{"notion"})
	assert.Empty(t, plugins)
	assert.Equal(t, []string{"notion"}, servers)
	assert.Empty(t, unknown)
}

func TestCatalog_Entries(t *testing.T) {
	assert.Equal(t, []Entry{
		{Name: "docs", Plugins: []string{"pdf@m", "lsp@m"}, McpServers: []string{}},
		{Name: "go", Plugins: []string{"go@m", "lsp@m"}, McpServers: []string{}},
	}, testCatalog().Entries())
	assert.Equal(t, []Entry{
		{Name: "docs", Plugins: []string{"pdf@m"}, McpServers: []string{"github"}},
		{Name: "go", Plugins: []string{"go@m"}, McpServers: []string{"gopls", "github"}},
		{Name: "notion", Plugins: []string{}, McpServers: []string{"notion"}},
	}, serverCatalog().Entries())
	assert.NotNil(t, Catalog(nil).Entries())
}

func TestCatalog_EntriesEmptyListsNonNil(t *testing.T) {
	entries := Catalog{"empty": {}}.Entries()
	require.Len(t, entries, 1)
	assert.NotNil(t, entries[0].Plugins)
	assert.Empty(t, entries[0].Plugins)
	assert.NotNil(t, entries[0].McpServers)
	assert.Empty(t, entries[0].McpServers)
}

func TestCatalog_EntriesReturnsCopy(t *testing.T) {
	c := serverCatalog()
	entries := c.Entries()
	entries[1].Plugins[0] = "changed"
	entries[1].McpServers[0] = "changed"
	assert.Equal(t, []string{"go@m"}, c["go"].Plugins)
	assert.Equal(t, []string{"gopls", "github"}, c["go"].McpServers)
}

func TestModule_ListsEntries(t *testing.T) {
	m := NewModule(Catalog{"go": {Plugins: []string{"go@m"}, McpServers: []string{"gopls"}}, "empty": {}})
	w := httptest.NewRecorder()
	m.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[{"name":"empty","plugins":[],"mcp_servers":[]},{"name":"go","plugins":["go@m"],"mcp_servers":["gopls"]}]}`, w.Body.String())
}

func TestBundle_JSONOmitsEmptyLists(t *testing.T) {
	data, err := json.Marshal(Bundle{})
	require.NoError(t, err)
	assert.JSONEq(t, `{}`, string(data))

	var b Bundle
	require.NoError(t, json.Unmarshal([]byte(`{"plugins":["go@m"],"mcp_servers":["gopls"]}`), &b))
	assert.Equal(t, Bundle{Plugins: []string{"go@m"}, McpServers: []string{"gopls"}}, b)
}

func TestUnion(t *testing.T) {
	assert.Equal(t, []string{"go", "docs", "web"}, Union([]string{"go", "docs"}, nil, []string{"docs", "web", "go"}))
	assert.Nil(t, Union())
	assert.Nil(t, Union([]string{}))
}
