package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type switchToBranchPickerMsg struct {
	workspace workspace.Workspace
}

type branchSelectedMsg struct {
	workspace workspace.Workspace
	branch    string
}

type branchItem struct {
	name string
}

func (i branchItem) Title() string       { return i.name }
func (i branchItem) Description() string { return "" }
func (i branchItem) FilterValue() string { return i.name }

type BranchPickerModel struct {
	list      list.Model
	workspace workspace.Workspace
}

func NewBranchPickerModel(ws workspace.Workspace) BranchPickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select base branch"
	l.KeyMap.Quit.SetEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, backKey}
	}
	return BranchPickerModel{list: l, workspace: ws}
}

func (m *BranchPickerModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m BranchPickerModel) Init() tea.Cmd {
	return nil
}

type branchesLoadedMsg struct {
	branches []string
}

func (m BranchPickerModel) Update(msg tea.Msg) (BranchPickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case branchesLoadedMsg:
		items := make([]list.Item, len(msg.branches))
		for i, b := range msg.branches {
			items[i] = branchItem{name: b}
		}
		cmd := m.list.SetItems(items)
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(branchItem); ok {
				ws := m.workspace
				branch := item.name
				return m, func() tea.Msg {
					return branchSelectedMsg{workspace: ws, branch: branch}
				}
			}
		case key.Matches(msg, backKey):
			return m, func() tea.Msg {
				return switchToNewSessionMsg{}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m BranchPickerModel) View() string {
	return m.list.View()
}
