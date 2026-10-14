package workspacepicker

import (
	"fmt"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	title             string
	multi             bool
	noAddDir          bool
	list              list.Model
	workspaces        []workspace.Workspace
	showHidden        bool
	activeWorkspaceID uint
	selected          []uint
	width, height     int
}

func New(title string, multi bool) Model {
	return Model{title: title, multi: multi, list: ui.NewList()}
}

func (m Model) WithoutAddDir() Model {
	m.noAddDir = true
	return m
}

func (m Model) Open(selected []uint) Model {
	m.selected = slices.Clone(selected)
	m.list.ResetFilter()
	m.rebuildItems()
	m.list.Select(0)
	ui.SkipUnselectable(&m.list, 0)
	return m
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.list.SetSize(width, max(height-2, 1))
}

func (m Model) CapturesText() bool {
	return ui.FilterTyping(m.list)
}

func sortWorkspaces(wss []workspace.Workspace, activeID uint) []workspace.Workspace {
	out := slices.Clone(wss)
	slices.SortStableFunc(out, func(a, b workspace.Workspace) int {
		if (a.ID == activeID) != (b.ID == activeID) {
			if a.ID == activeID {
				return -1
			}
			return 1
		}
		return b.LastUsedAt.Compare(a.LastUsedAt)
	})
	return out
}

func (m *Model) rebuildItems() {
	var items []list.Item
	for _, ws := range sortWorkspaces(m.workspaces, m.activeWorkspaceID) {
		if !m.showHidden && ws.IsHidden {
			continue
		}
		items = append(items, workspaceItem{workspace: ws, multi: m.multi, checked: slices.Contains(m.selected, ws.ID)})
	}
	ui.SetItems(&m.list, items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case provider.WorkspacesStateUpdatedMsg:
		m.activeWorkspaceID = msg.ActiveWorkspaceID
		m.workspaces = msg.Workspaces
		m.rebuildItems()
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

func (m Model) cursor() (workspace.Workspace, bool) {
	item, ok := m.list.SelectedItem().(workspaceItem)
	return item.workspace, ok
}

func (m Model) byID(id uint) (workspace.Workspace, bool) {
	for _, ws := range m.workspaces {
		if ws.ID == id {
			return ws, true
		}
	}
	return workspace.Workspace{}, false
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch {
	case m.multi && key.Matches(msg, Keys.Toggle):
		ws, ok := m.cursor()
		if !ok {
			return m, nil, true
		}
		if i := slices.Index(m.selected, ws.ID); i >= 0 {
			m.selected = slices.Delete(m.selected, i, i+1)
		} else {
			m.selected = append(m.selected, ws.ID)
		}
		m.rebuildItems()
		return m, nil, true
	case key.Matches(msg, Keys.Select):
		var picked []workspace.Workspace
		if m.multi {
			for _, id := range m.selected {
				if ws, ok := m.byID(id); ok {
					picked = append(picked, ws)
				}
			}
		}
		if len(picked) == 0 {
			if ws, ok := m.cursor(); ok {
				picked = []workspace.Workspace{ws}
			}
		}
		if len(picked) == 0 {
			return m, nil, true
		}
		return m, func() tea.Msg { return SelectedMsg{Workspaces: picked} }, true
	case key.Matches(msg, Keys.ToggleHidden):
		m.showHidden = !m.showHidden
		m.rebuildItems()
		return m, nil, true
	case !m.noAddDir && key.Matches(msg, Keys.AddDir):
		return m, func() tea.Msg { return AddDirectoryMsg{} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	title := ui.FormTitle(m.title)
	if m.multi && len(m.selected) > 0 {
		title += ui.Muted().Render(fmt.Sprintf("  %d selected", len(m.selected)))
	}
	title = ui.Clamp(title, m.width)
	body := ui.ListView(m.list)
	if len(m.list.Items()) == 0 {
		body = ui.Muted().Render("  No workspaces")
	}
	return ui.FitHeight(title+"\n\n"+body, m.height)
}
