package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type workspaceItem struct {
	workspace workspace.Workspace
}

func (i workspaceItem) Title() string       { return i.workspace.Name }
func (i workspaceItem) Description() string { return abbreviatePath(i.workspace.Path) }
func (i workspaceItem) FilterValue() string { return i.workspace.Name }

type WorkspacePickerModel struct {
	list              list.Model
	sortActiveFirst   bool
	activeWorkspaceID string
}

func NewWorkspacePickerModel(title string, sortActiveFirst bool) WorkspacePickerModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return WorkspacePickerModel{
		list:            l,
		sortActiveFirst: sortActiveFirst,
	}
}

func (m *WorkspacePickerModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m WorkspacePickerModel) Init() (WorkspacePickerModel, tea.Cmd) {
	return m, nil
}

func (m WorkspacePickerModel) OnWindowSizeMsg(msg tea.WindowSizeMsg) (WorkspacePickerModel, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m WorkspacePickerModel) Keys() help.KeyMap {
	return workspacePickerKeys
}

func (m WorkspacePickerModel) Update(msg tea.Msg) (WorkspacePickerModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.activeWorkspaceID
		workspaces := msg.workspaces

		if m.sortActiveFirst && m.activeWorkspaceID != "" {
			sorted := make([]workspace.Workspace, 0, len(workspaces))
			var rest []workspace.Workspace
			for _, ws := range workspaces {
				if ws.ID == m.activeWorkspaceID {
					sorted = append(sorted, ws)
				} else {
					rest = append(rest, ws)
				}
			}
			workspaces = append(sorted, rest...)
		}

		items := make([]list.Item, len(workspaces))
		for i, ws := range workspaces {
			items[i] = workspaceItem{workspace: ws}
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

func (m WorkspacePickerModel) OnKeyMsg(msg tea.KeyMsg) (WorkspacePickerModel, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	switch {
	case key.Matches(msg, workspacePickerKeys.Select):
		if item, ok := m.list.SelectedItem().(workspaceItem); ok {
			ws := item.workspace
			return m, func() tea.Msg {
				return workspaceSelectedMsg{workspace: ws}
			}, true
		}
	case key.Matches(msg, workspacePickerKeys.AddDir):
		return m, func() tea.Msg { return addDirectoryRequestMsg{} }, true
	}
	return m, nil, false
}

func (m WorkspacePickerModel) View() string {
	return m.list.View()
}

func abbreviatePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
