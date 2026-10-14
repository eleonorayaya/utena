package workspacelist

import (
	"slices"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type Model struct {
	list             list.Model
	workspaces       []workspace.Workspace
	pendingDeleteID  uint
	pendingMigrateID uint
	showHidden       bool
	status           ui.Status
	width, height    int
}

func New() Model {
	return Model{list: ui.NewList()}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.pendingDeleteID, m.pendingMigrateID = 0, 0
	m.status = ui.Status{}
	return m, provider.FetchWorkspaces()
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) Status() ui.Status {
	return m.status
}

func (m Model) CapturesText() bool {
	return m.list.FilterState() == list.Filtering
}

func byLastUsed(wss []workspace.Workspace) []workspace.Workspace {
	out := slices.Clone(wss)
	slices.SortStableFunc(out, func(a, b workspace.Workspace) int { return b.LastUsedAt.Compare(a.LastUsedAt) })
	return out
}

func (m *Model) rebuildItems() {
	var visible, hidden []workspace.Workspace
	repoW := 0
	for _, ws := range m.workspaces {
		if ws.IsHidden {
			if !m.showHidden {
				continue
			}
			hidden = append(hidden, ws)
		} else {
			visible = append(visible, ws)
		}
		repoW = max(repoW, lipgloss.Width(repoName(ws)))
	}
	repoW = min(repoW, maxRepoColW) + 2

	cur := m.selectedID()
	var items []list.Item
	if len(visible) > 0 {
		items = append(items, ui.SectionItem{Title: "Workspaces", Count: len(visible)})
		for _, ws := range byLastUsed(visible) {
			items = append(items, workspaceItem{workspace: ws, repoW: repoW})
		}
	}
	if len(hidden) > 0 {
		if len(items) > 0 {
			items = append(items, ui.SpacerItem{})
		}
		items = append(items, ui.SectionItem{Title: "Hidden", Count: len(hidden)})
		for _, ws := range byLastUsed(hidden) {
			items = append(items, workspaceItem{workspace: ws, repoW: repoW})
		}
	}
	ui.SetItems(&m.list, items)
	m.selectID(cur)
}

func (m Model) selected() (workspace.Workspace, bool) {
	item, ok := m.list.SelectedItem().(workspaceItem)
	return item.workspace, ok
}

func (m Model) selectedID() uint {
	ws, ok := m.selected()
	if !ok {
		return 0
	}
	return ws.ID
}

func (m *Model) selectID(id uint) {
	ui.SelectWhere(&m.list, func(it list.Item) bool {
		w, ok := it.(workspaceItem)
		return ok && w.workspace.ID == id
	})
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.list.SetSize(msg.Width, max(msg.Height-2, 1))
		return m, nil
	case provider.WorkspacesStateUpdatedMsg:
		m.workspaces = msg.Workspaces
		m.rebuildItems()
		return m, nil
	case provider.ErrMsg:
		m.status = ui.Status{Text: msg.Err.Error(), Kind: ui.StatusError}
		return m, nil
	case tea.KeyMsg:
		if m.list.FilterState() != list.Filtering {
			if mm, cmd, handled := m.OnKeyMsg(msg); handled {
				return mm, cmd
			}
		}
	}
	var cmd tea.Cmd
	m.list, cmd = ui.UpdateList(m.list, msg)
	return m, cmd
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if !key.Matches(msg, keys.Delete) {
		m.pendingDeleteID = 0
	}
	if !key.Matches(msg, keys.Migrate) {
		m.pendingMigrateID = 0
	}
	m.status = ui.Status{}

	switch {
	case key.Matches(msg, keys.Hide):
		ws, ok := m.selected()
		if !ok {
			return m, nil, true
		}
		verb := "hidden"
		if ws.IsHidden {
			verb = "unhidden"
		}
		m.status = ui.Status{Text: ws.Name + " " + verb}
		return m, provider.SetWorkspaceHidden(ws.ID, !ws.IsHidden), true
	case key.Matches(msg, keys.ToggleHidden):
		m.showHidden = !m.showHidden
		m.rebuildItems()
		return m, nil, true
	case key.Matches(msg, keys.Delete):
		ws, ok := m.selected()
		if !ok {
			return m, nil, true
		}
		if m.pendingDeleteID == ws.ID {
			m.pendingDeleteID = 0
			return m, provider.DeleteWorkspace(ws.ID), true
		}
		m.pendingDeleteID = ws.ID
		m.status = ui.Status{Text: "press d again to delete " + ws.Name, Kind: ui.StatusWarning}
		return m, nil, true
	case key.Matches(msg, keys.Migrate):
		ws, ok := m.selected()
		if !ok {
			return m, nil, true
		}
		switch {
		case !ws.IsGitRepo:
			m.status = ui.Status{Text: ws.Name + " is not a git repository"}
			return m, nil, true
		case ws.IsBare:
			m.status = ui.Status{Text: ws.Name + " is already bare"}
			return m, nil, true
		case ws.IsBusy():
			m.status = ui.Status{Text: ws.Name + " is busy"}
			return m, nil, true
		}
		if m.pendingMigrateID == ws.ID {
			m.pendingMigrateID = 0
			m.status = ui.Status{Text: "migrating " + ws.Name + " to bare…"}
			return m, provider.MigrateWorkspaceToBare(ws.ID), true
		}
		m.pendingMigrateID = ws.ID
		m.status = ui.Status{Text: "press m again to migrate " + ws.Name + " to bare", Kind: ui.StatusWarning}
		return m, nil, true
	case key.Matches(msg, keys.CloneFromURL):
		return m, router.NavigateTo(router.WorkspaceCloneFormView), true
	case key.Matches(msg, keys.Back):
		if ui.FilterActive(m.list) {
			return m, nil, false
		}
		return m, router.Back(), true
	}
	return m, nil, false
}

func (m Model) View() string {
	muted := ui.Muted()
	if len(m.list.Items()) == 0 {
		return ui.FitHeight(muted.Render("  No workspaces — press c to clone one"), m.height)
	}
	body := ui.FitHeight(ui.ListView(m.list), max(m.height-2, 1))
	var path string
	if ws, ok := m.selected(); ok {
		path = ui.Truncate(ui.AbbreviatePath(ws.Path), max(m.width-2, 1))
	}
	return ui.FitHeight(body+"\n\n"+"  "+muted.Render(path), m.height)
}
