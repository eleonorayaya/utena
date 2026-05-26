package workspacelist

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/workspacedetail"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	list            list.Model
	workspaces      []workspace.Workspace
	pendingDeleteID uint
	showHidden      bool
	height          int
}

func New() Model {
	return Model{list: ulist.NewWithDelegate("Workspaces", compactDelegate{})}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, provider.FetchWorkspaces()
}

func (m Model) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m *Model) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, ws := range m.workspaces {
		if !m.showHidden && ws.IsHidden {
			continue
		}
		items = append(items, workspaceItem{workspace: ws})
	}
	return m.list.SetItems(items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.list.SetWidth(msg.Width)
		m.list.SetHeight(msg.Height)
		return m, nil
	case provider.WorkspacesStateUpdatedMsg:
		m.workspaces = msg.Workspaces
		return m, m.rebuildItems()
	case provider.ErrMsg:
		return m, m.list.NewStatusMessage(msg.Err.Error())
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
	if !key.Matches(msg, keys.Delete) {
		m.pendingDeleteID = 0
	}
	switch {
	case key.Matches(msg, keys.Select):
		item, ok := m.list.SelectedItem().(workspaceItem)
		if !ok {
			return m, nil, false
		}
		return m, tea.Sequence(
			router.NavigateTo(router.WorkspaceDetailView),
			workspacedetail.Select(item.workspace),
		), true
	case key.Matches(msg, keys.Hide):
		item, ok := m.list.SelectedItem().(workspaceItem)
		if !ok {
			return m, nil, false
		}
		newHidden := !item.workspace.IsHidden
		verb := "hidden"
		if !newHidden {
			verb = "unhidden"
		}
		return m, tea.Batch(
			provider.SetWorkspaceHidden(item.workspace.ID, newHidden),
			m.list.NewStatusMessage(fmt.Sprintf("%s %s", item.workspace.Name, verb)),
		), true
	case key.Matches(msg, keys.ToggleHidden):
		m.showHidden = !m.showHidden
		msg := "showing all workspaces"
		if !m.showHidden {
			msg = "hiding hidden workspaces"
		}
		return m, tea.Batch(m.rebuildItems(), m.list.NewStatusMessage(msg)), true
	case key.Matches(msg, keys.Delete):
		item, ok := m.list.SelectedItem().(workspaceItem)
		if !ok {
			return m, nil, false
		}
		if m.pendingDeleteID == item.workspace.ID {
			m.pendingDeleteID = 0
			return m, provider.DeleteWorkspace(item.workspace.ID), true
		}
		m.pendingDeleteID = item.workspace.ID
		return m, m.list.NewStatusMessage(fmt.Sprintf("press d again to delete %s", item.workspace.Name)), true
	case key.Matches(msg, keys.CloneFromURL):
		return m, router.NavigateTo(router.WorkspaceCloneFormView), true
	case key.Matches(msg, keys.Back):
		return m, router.Back(), true
	}
	return m, nil, false
}

func padToHeight(s string, h int) string {
	if h <= 0 {
		return s
	}
	n := strings.Count(s, "\n")
	if n >= h {
		return s
	}
	return s + strings.Repeat("\n", h-n)
}

func (m Model) View() string {
	v := strings.TrimRight(m.list.View(), "\n")
	return padToHeight(v, m.height-1)
}
