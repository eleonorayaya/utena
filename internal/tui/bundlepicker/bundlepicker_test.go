package bundlepicker

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/tui/uitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func catalog() []bundles.Entry {
	return []bundles.Entry{
		{Name: "docs", Plugins: []string{"pdf@m"}},
		{Name: "go", Plugins: []string{"go@m", "gopls@m"}},
	}
}

func opened(selected ...string) Model {
	m := New("Bundles")
	m.SetSize(60, 10)
	return m.SetCatalog(catalog()).Open(selected)
}

func space() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")} }

func feed(m Model, msg tea.Msg) Model {
	m, cmd := m.Update(msg)
	for _, out := range uitest.Drain(cmd) {
		if fm, ok := out.(list.FilterMatchesMsg); ok {
			m, _ = m.Update(fm)
		}
	}
	return m
}

func confirm(t *testing.T, m Model) SelectedMsg {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msgs := uitest.Drain(cmd)
	require.Len(t, msgs, 1)
	sel, ok := msgs[0].(SelectedMsg)
	require.True(t, ok)
	return sel
}

func assertFits(t *testing.T, view string, width int) {
	t.Helper()
	for _, l := range strings.Split(view, "\n") {
		assert.LessOrEqual(t, lipgloss.Width(l), width)
	}
}

func TestPicker_ShowsBundlesWithPlugins(t *testing.T) {
	v := opened().View()
	assert.Contains(t, v, "[ ] docs")
	assert.Contains(t, v, "[ ] go")
	assert.Contains(t, v, "go@m, gopls@m")
	assertFits(t, v, 60)
}

func TestPicker_LongPluginListFitsWidth(t *testing.T) {
	m := New("Bundles")
	m.SetSize(40, 10)
	m = m.SetCatalog([]bundles.Entry{{Name: "everything", Plugins: []string{
		"superlongpluginname-one@marketplace",
		"superlongpluginname-two@marketplace",
		"superlongpluginname-three@marketplace",
	}}}).Open(nil)
	assertFits(t, m.View(), 40)
}

func TestPicker_ServersOnlyBundleShowsMcpServers(t *testing.T) {
	m := New("Bundles")
	m.SetSize(60, 10)
	m = m.SetCatalog([]bundles.Entry{{Name: "cad", McpServers: []string{"zoo"}}}).Open(nil)
	v := m.View()
	assert.Contains(t, v, "zoo (mcp)")
	assertFits(t, v, 60)
}

func TestPicker_MixedBundleShowsPluginsThenServers(t *testing.T) {
	m := New("Bundles")
	m.SetSize(80, 10)
	m = m.SetCatalog([]bundles.Entry{{Name: "homelab", Plugins: []string{"ha@m"}, McpServers: []string{"ha", "zoo"}}}).Open(nil)
	v := m.View()
	assert.Contains(t, v, "ha@m, ha (mcp), zoo (mcp)")
	assertFits(t, v, 80)
}

func TestPicker_LongServerListFitsWidth(t *testing.T) {
	m := New("Bundles")
	m.SetSize(40, 10)
	m = m.SetCatalog([]bundles.Entry{{Name: "everything", Plugins: []string{"superlongpluginname@marketplace"}, McpServers: []string{
		"superlongservername-one",
		"superlongservername-two",
	}}}).Open(nil)
	assertFits(t, m.View(), 40)
}

func TestPicker_PreChecksSelected(t *testing.T) {
	v := opened("go").View()
	assert.Contains(t, v, "[x] go")
	assert.Contains(t, v, "1 selected")
}

func TestPicker_ToggleAndConfirm(t *testing.T) {
	m := opened()
	m, _ = m.Update(space())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(space())
	assert.Equal(t, SelectedMsg{Bundles: []string{"docs", "go"}}, confirm(t, m))
}

func TestPicker_UntoggleEverythingConfirmsEmpty(t *testing.T) {
	m := opened("docs")
	m, _ = m.Update(space())
	sel := confirm(t, m)
	assert.NotNil(t, sel.Bundles)
	assert.Empty(t, sel.Bundles)
}

func TestPicker_UnknownSelectedNameIsListed(t *testing.T) {
	m := opened("legacy")
	v := m.View()
	assert.Contains(t, v, "[x] legacy")
	assert.Contains(t, v, "unknown bundle")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(space())
	sel := confirm(t, m)
	assert.NotNil(t, sel.Bundles)
	assert.Empty(t, sel.Bundles)
}

func TestPicker_SetCatalogKeepsToggles(t *testing.T) {
	m := New("Bundles")
	m.SetSize(60, 10)
	m = m.Open([]string{"go"})
	assert.Contains(t, m.View(), "unknown bundle")
	m = m.SetCatalog(catalog())
	v := m.View()
	assert.Contains(t, v, "[x] go")
	assert.Contains(t, v, "docs")
	assert.NotContains(t, v, "unknown bundle")
	assert.Equal(t, SelectedMsg{Bundles: []string{"go"}}, confirm(t, m))
}

func TestPicker_SetCatalogKeepsCursorAndFilter(t *testing.T) {
	m := opened()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m.SetCatalog(catalog())
	m, _ = m.Update(space())
	assert.Equal(t, SelectedMsg{Bundles: []string{"go"}}, confirm(t, m))

	m = opened()
	m = feed(m, uitest.Runes("/"))
	m = feed(m, uitest.Runes("g"))
	m = m.SetCatalog(catalog())
	assert.True(t, m.CapturesText())
}

func TestPicker_CatalogGetter(t *testing.T) {
	m := New("Bundles")
	assert.Nil(t, m.Catalog())
	assert.Equal(t, catalog(), m.SetCatalog(catalog()).Catalog())
	assert.Equal(t, catalog(), m.SetCatalog(catalog()).Open([]string{"go"}).Catalog())
}

func TestPicker_EscCloses(t *testing.T) {
	_, cmd := opened().Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, []tea.Msg{ClosedMsg{}}, uitest.Drain(cmd))
}

func TestPicker_EmptyCatalog(t *testing.T) {
	m := New("Bundles")
	m.SetSize(60, 10)
	m = m.Open(nil)
	assert.Contains(t, m.View(), "No bundles configured")
}

func TestPicker_Filters(t *testing.T) {
	m := opened()
	m = feed(m, uitest.Runes("/"))
	m = feed(m, uitest.Runes("g"))
	m = feed(m, uitest.Runes("o"))
	assert.True(t, m.CapturesText())
	v := m.View()
	assert.Contains(t, v, "gopls@m")
	assert.NotContains(t, v, "pdf@m")
}

func TestPicker_SpaceWhileFilteringDoesNotToggle(t *testing.T) {
	m := opened()
	m = feed(m, uitest.Runes("/"))
	m = feed(m, space())
	assert.NotContains(t, m.View(), "selected")
}
