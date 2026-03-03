package branchpicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
)

type Model struct {
	list list.Model
}

func New() Model {
	return Model{list: ulist.New("Select base branch")}
}

func (m *Model) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m Model) Keys() help.KeyMap {
	return Keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provider.BranchesLoadedMsg:
		items := make([]list.Item, len(msg.Branches))
		for i, b := range msg.Branches {
			items[i] = branchItem{name: b}
		}
		return m, m.list.SetItems(items)

	case tea.KeyMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.OnKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	if key.Matches(msg, Keys.Select) {
		if item, ok := m.list.SelectedItem().(branchItem); ok {
			branch := item.name
			return m, func() tea.Msg {
				return SelectedMsg{Branch: branch}
			}, true
		}
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
