package tui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type switchToNameInputMsg struct {
	workspace workspace.Workspace
}

type switchToSessionListMsg struct{}

type workspaceItem struct {
	workspace workspace.Workspace
}

func (i workspaceItem) Title() string       { return i.workspace.Name }
func (i workspaceItem) Description() string { return abbreviatePath(i.workspace.Path) }
func (i workspaceItem) FilterValue() string { return i.workspace.Name }

type NewSessionModel struct {
	list list.Model
}

func NewNewSessionModel() NewSessionModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Select workspace"
	l.KeyMap.Quit.SetEnabled(false)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, backKey}
	}
	return NewSessionModel{list: l}
}

func (m *NewSessionModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m NewSessionModel) Init() tea.Cmd {
	return nil
}

func (m NewSessionModel) Update(msg tea.Msg) (NewSessionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesLoadedMsg:
		items := make([]list.Item, len(msg.workspaces))
		for i, ws := range msg.workspaces {
			items[i] = workspaceItem{workspace: ws}
		}
		cmd := m.list.SetItems(items)
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(workspaceItem); ok {
				return m, func() tea.Msg {
					return switchToNameInputMsg{workspace: item.workspace}
				}
			}
		case key.Matches(msg, backKey):
			return m, func() tea.Msg {
				return switchToSessionListMsg{}
			}
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m NewSessionModel) View() string {
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
