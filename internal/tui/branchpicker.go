package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type branchItem struct {
	name string
}

func (i branchItem) Title() string       { return i.name }
func (i branchItem) Description() string { return "" }
func (i branchItem) FilterValue() string { return i.name }

type BranchPickerModel struct {
	list list.Model
}

func NewBranchPickerModel() BranchPickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select base branch"
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return BranchPickerModel{list: l}
}

func (m *BranchPickerModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m BranchPickerModel) OnWindowSizeMsg(msg tea.WindowSizeMsg) (BranchPickerModel, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m BranchPickerModel) Keys() help.KeyMap {
	return branchPickerKeys
}

func (m BranchPickerModel) Update(msg tea.Msg) (BranchPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case branchesLoadedMsg:
		items := make([]list.Item, len(msg.branches))
		for i, b := range msg.branches {
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

func (m BranchPickerModel) OnKeyMsg(msg tea.KeyMsg) (BranchPickerModel, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	if key.Matches(msg, branchPickerKeys.Select) {
		if item, ok := m.list.SelectedItem().(branchItem); ok {
			branch := item.name
			return m, func() tea.Msg {
				return branchSelectedMsg{branch: branch}
			}, true
		}
	}
	return m, nil, false
}

func (m BranchPickerModel) View() string {
	return m.list.View()
}
