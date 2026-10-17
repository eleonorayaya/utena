package bundles

import (
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

func TestCatalog_ManagedPlugins(t *testing.T) {
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, testCatalog().ManagedPlugins())
	assert.Empty(t, Catalog(nil).ManagedPlugins())
}

func TestCatalog_Resolve(t *testing.T) {
	plugins, unknown := testCatalog().Resolve([]string{"docs", "nope", "go"})
	assert.Equal(t, []string{"go@m", "lsp@m", "pdf@m"}, plugins)
	assert.Equal(t, []string{"nope"}, unknown)

	plugins, unknown = Catalog(nil).Resolve([]string{"go"})
	assert.Empty(t, plugins)
	assert.Equal(t, []string{"go"}, unknown)
}

func TestCatalog_Entries(t *testing.T) {
	assert.Equal(t, []Entry{
		{Name: "docs", Plugins: []string{"pdf@m", "lsp@m"}},
		{Name: "go", Plugins: []string{"go@m", "lsp@m"}},
	}, testCatalog().Entries())
	assert.NotNil(t, Catalog(nil).Entries())
}

func TestCatalog_EntriesEmptyPluginsNonNil(t *testing.T) {
	entries := Catalog{"empty": {}}.Entries()
	require.Len(t, entries, 1)
	assert.NotNil(t, entries[0].Plugins)
	assert.Empty(t, entries[0].Plugins)
}

func TestCatalog_EntriesReturnsCopy(t *testing.T) {
	c := testCatalog()
	entries := c.Entries()
	entries[1].Plugins[0] = "changed"
	assert.Equal(t, []string{"go@m", "lsp@m"}, c["go"].Plugins)
}

func TestModule_ListsEntries(t *testing.T) {
	m := NewModule(Catalog{"go": {Plugins: []string{"go@m"}}, "empty": {}})
	w := httptest.NewRecorder()
	m.Routes().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"bundles":[{"name":"empty","plugins":[]},{"name":"go","plugins":["go@m"]}]}`, w.Body.String())
}

func TestUnion(t *testing.T) {
	assert.Equal(t, []string{"go", "docs", "web"}, Union([]string{"go", "docs"}, nil, []string{"docs", "web", "go"}))
	assert.Nil(t, Union())
	assert.Nil(t, Union([]string{}))
}
