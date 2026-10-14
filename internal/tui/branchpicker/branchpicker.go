package branchpicker

import (
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	title         string
	workspaceID   uint
	list          list.Model
	local         []string
	remote        []string
	defaultBranch string
	fetching      bool
	status        ui.Status
	width, height int
}

func New() Model {
	return Model{list: ui.NewList()}
}

func (m Model) Open(title string, workspaceID uint, local, remote []string, defaultBranch string, current string) Model {
	m.title = title
	m.workspaceID = workspaceID
	m.local = local
	m.remote = remote
	m.defaultBranch = defaultBranch
	m.fetching = false
	m.status = ui.Status{}
	m.list.ResetFilter()
	m.rebuildItems()
	m.selectBranch(current)
	return m
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.list.SetSize(width, max(height-2, 1))
}

func (m Model) WorkspaceID() uint {
	return m.workspaceID
}

func (m Model) Keys() help.KeyMap {
	return Keys
}

func (m Model) Status() ui.Status {
	return m.status
}

func (m Model) CapturesText() bool {
	return ui.FilterTyping(m.list)
}

func (m *Model) rebuildItems() {
	local := slices.Sorted(slices.Values(m.local))
	isLocal := make(map[string]bool, len(local))
	for _, b := range local {
		isLocal[b] = true
	}
	var remote []string
	for _, r := range m.remote {
		if !isLocal[r] {
			remote = append(remote, r)
		}
	}
	slices.Sort(remote)

	var items []list.Item
	items = append(items, ui.SectionItem{Title: "Local", Count: len(local)})
	for _, b := range local {
		items = append(items, branchItem{name: b, isDefault: b == m.defaultBranch})
	}
	if len(remote) > 0 {
		items = append(items, ui.SpacerItem{}, ui.SectionItem{Title: "Remote", Count: len(remote)})
		for _, b := range remote {
			items = append(items, branchItem{name: b, isDefault: b == m.defaultBranch})
		}
	}
	ui.SetItems(&m.list, items)
}

func (m *Model) selectBranch(name string) {
	ui.SelectWhere(&m.list, func(it list.Item) bool {
		b, ok := it.(branchItem)
		return ok && b.name == name
	})
}

func (m Model) selected() string {
	if b, ok := m.list.SelectedItem().(branchItem); ok {
		return b.name
	}
	return ""
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provider.BranchesLoadedMsg:
		if msg.WorkspaceID != m.workspaceID || msg.Err != nil {
			return m, nil
		}
		cur := m.selected()
		m.local = msg.Branches
		m.defaultBranch = msg.DefaultBranch
		m.rebuildItems()
		m.selectBranch(cur)
		return m, nil
	case provider.BranchesFetchedMsg:
		if msg.WorkspaceID != m.workspaceID {
			return m, nil
		}
		m.fetching = false
		if msg.Err != nil {
			m.status = ui.Status{Text: "fetch failed: " + msg.Err.Error(), Kind: ui.StatusError}
			return m, nil
		}
		m.status = ui.Status{}
		cur := m.selected()
		m.local, m.remote = provider.SplitBranches(msg.Branches)
		if msg.DefaultBranch != "" {
			m.defaultBranch = msg.DefaultBranch
		}
		m.rebuildItems()
		m.selectBranch(cur)
		return m, nil
	case tea.KeyMsg:
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
	case key.Matches(msg, Keys.Select):
		name := m.selected()
		if name == "" {
			return m, nil, true
		}
		return m, func() tea.Msg { return SelectedMsg{Branch: name} }, true
	case key.Matches(msg, Keys.Fetch):
		if m.fetching {
			return m, nil, true
		}
		m.fetching = true
		m.status = ui.Status{Text: "fetching origin…"}
		return m, func() tea.Msg { return FetchRequestedMsg{} }, true
	case key.Matches(msg, Keys.Manual):
		return m, func() tea.Msg { return ManualEntryRequestedMsg{} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	body := ui.ListView(m.list)
	if len(m.local)+len(m.remote) == 0 {
		body = ui.Muted().Render("  No branches — press r to fetch or n to type one")
	}
	return ui.FitHeight(ui.Clamp(ui.FormTitle(m.title), m.width)+"\n\n"+body, m.height)
}
