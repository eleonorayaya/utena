package workspacepicker

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	list              list.Model
	workspaces        []workspace.Workspace
	sortActiveFirst   bool
	showHidden        bool
	activeWorkspaceID uint
	selected          map[uint]struct{}
	selectionOrder    []uint
}

func New(title string, sortActiveFirst bool) Model {
	return Model{
		list:            ulist.NewWithDelegate(title, compactPickerDelegate{}),
		sortActiveFirst: sortActiveFirst,
		selected:        make(map[uint]struct{}),
	}
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

func (m *Model) rebuildItems() tea.Cmd {
	workspaces := m.workspaces

	if m.sortActiveFirst && m.activeWorkspaceID != 0 {
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

	items := make([]list.Item, 0, len(workspaces))
	for _, ws := range workspaces {
		if !m.showHidden && ws.IsHidden {
			continue
		}
		_, isSelected := m.selected[ws.ID]
		items = append(items, workspaceItem{workspace: ws, selected: isSelected})
	}
	return m.list.SetItems(items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provider.WorkspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.ActiveWorkspaceID
		m.workspaces = msg.Workspaces
		return m, m.rebuildItems()

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

func (m Model) selectedWorkspaces() []workspace.Workspace {
	if len(m.selectionOrder) == 0 {
		return nil
	}
	byID := make(map[uint]workspace.Workspace, len(m.workspaces))
	for _, ws := range m.workspaces {
		byID[ws.ID] = ws
	}
	out := make([]workspace.Workspace, 0, len(m.selectionOrder))
	for _, id := range m.selectionOrder {
		if ws, ok := byID[id]; ok {
			out = append(out, ws)
		}
	}
	return out
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	switch {
	case key.Matches(msg, Keys.Toggle):
		if item, ok := m.list.SelectedItem().(workspaceItem); ok {
			id := item.workspace.ID
			if _, exists := m.selected[id]; exists {
				delete(m.selected, id)
				for i, sid := range m.selectionOrder {
					if sid == id {
						m.selectionOrder = append(m.selectionOrder[:i], m.selectionOrder[i+1:]...)
						break
					}
				}
			} else {
				m.selected[id] = struct{}{}
				m.selectionOrder = append(m.selectionOrder, id)
			}
			return m, m.rebuildItems(), true
		}
	case key.Matches(msg, Keys.Select):
		var cursorWS *workspace.Workspace
		if item, ok := m.list.SelectedItem().(workspaceItem); ok {
			ws := item.workspace
			cursorWS = &ws
		}
		if cursorWS != nil {
			if _, exists := m.selected[cursorWS.ID]; !exists {
				m.selected[cursorWS.ID] = struct{}{}
				m.selectionOrder = append(m.selectionOrder, cursorWS.ID)
			}
		}
		multi := m.selectedWorkspaces()
		if len(multi) == 0 && cursorWS != nil {
			multi = []workspace.Workspace{*cursorWS}
		}
		if len(multi) == 0 {
			return m, nil, true
		}
		return m, func() tea.Msg {
			return SelectedMsg{Workspace: multi[0], Workspaces: multi}
		}, true
	case key.Matches(msg, Keys.ToggleHidden):
		m.showHidden = !m.showHidden
		statusMsg := "showing all workspaces"
		if !m.showHidden {
			statusMsg = "hiding hidden workspaces"
		}
		return m, tea.Batch(m.rebuildItems(), m.list.NewStatusMessage(statusMsg)), true
	case key.Matches(msg, Keys.AddDir):
		return m, func() tea.Msg { return AddDirectoryMsg{} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
