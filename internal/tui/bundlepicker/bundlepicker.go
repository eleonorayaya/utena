package bundlepicker

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/bundles"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	title         string
	list          list.Model
	catalog       []bundles.Entry
	selected      []string
	width, height int
}

func New(title string) Model {
	return Model{title: title, list: ui.NewList()}
}

func (m Model) Open(selected []string) Model {
	m.selected = slices.Clone(selected)
	m.list.ResetFilter()
	m.rebuildItems()
	m.list.Select(0)
	ui.SkipUnselectable(&m.list, 0)
	return m
}

func (m Model) Catalog() []bundles.Entry {
	return m.catalog
}

func (m Model) SetCatalog(catalog []bundles.Entry) Model {
	m.catalog = catalog
	m.rebuildItems()
	ui.SkipUnselectable(&m.list, m.list.Index())
	return m
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.list.SetSize(width, max(height-2, 1))
}

func (m Model) CapturesText() bool {
	return ui.FilterTyping(m.list)
}

func (m Model) entries() []bundleItem {
	var out []bundleItem
	known := make(map[string]struct{}, len(m.catalog))
	for _, e := range m.catalog {
		known[e.Name] = struct{}{}
		out = append(out, bundleItem{name: e.Name, plugins: e.Plugins, servers: e.McpServers})
	}
	for _, name := range m.selected {
		if _, ok := known[name]; ok {
			continue
		}
		known[name] = struct{}{}
		out = append(out, bundleItem{name: name, unknown: true})
	}
	return out
}

func (m *Model) rebuildItems() {
	entries := m.entries()
	nameW := 0
	for _, e := range entries {
		nameW = max(nameW, lipgloss.Width(e.name)+2)
	}
	items := make([]list.Item, 0, len(entries))
	for _, e := range entries {
		e.checked = slices.Contains(m.selected, e.name)
		e.nameW = nameW
		items = append(items, e)
	}
	ui.SetItems(&m.list, items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		if msg.Type == tea.KeyEsc && !ui.FilterActive(m.list) {
			return m, func() tea.Msg { return ClosedMsg{} }
		}
		if !ui.FilterTyping(m.list) {
			if mm, cmd, ok := m.onKey(msg); ok {
				return mm, cmd
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = ui.UpdateList(m.list, msg)
	return m, cmd
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, Keys.Toggle):
		item, ok := m.list.SelectedItem().(bundleItem)
		if !ok {
			return m, nil, true
		}
		m.selected = ui.ToggleSelected(m.selected, item.name)
		m.rebuildItems()
		return m, nil, true
	case key.Matches(msg, Keys.Select):
		picked := []string{}
		for _, e := range m.entries() {
			if slices.Contains(m.selected, e.name) {
				picked = append(picked, e.name)
			}
		}
		return m, func() tea.Msg { return SelectedMsg{Bundles: picked} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	title := ui.FormTitle(m.title)
	if len(m.selected) > 0 {
		title += ui.Muted().Render(fmt.Sprintf("  %d selected", len(m.selected)))
	}
	title = ui.Clamp(title, m.width)
	body := ui.ListView(m.list)
	if len(m.list.Items()) == 0 {
		body = ui.Muted().Render(`  No bundles configured — add "bundles" to daemon.json`)
	}
	return ui.FitHeight(title+"\n\n"+body, m.height)
}
