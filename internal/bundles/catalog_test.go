package bundles

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestUnion(t *testing.T) {
	assert.Equal(t, []string{"go", "docs", "web"}, Union([]string{"go", "docs"}, nil, []string{"docs", "web", "go"}))
	assert.Nil(t, Union())
	assert.Nil(t, Union([]string{}))
}
