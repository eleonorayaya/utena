package sessionlist

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

type Model struct {
	list            list.Model
	sessions        []session.Session
	claudeSessions  map[string][]claude.ClaudeSession
	pendingDeleteID string
	showDead        bool
}

func New() Model {
	l := ulist.New("Sessions")
	l.KeyMap.Quit.SetEnabled(true)
	return Model{list: l}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, provider.FetchSessions()
}

func (m Model) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m *Model) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, s := range m.sessions {
		if s.IsDeleted {
			continue
		}
		if s.IsDead && !m.showDead {
			continue
		}
		status := aggregateClaudeStatus(m.claudeSessions[s.ID])
		items = append(items, sessionItem{session: s, claudeStatus: status})
	}
	return m.list.SetItems(items)
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		m.claudeSessions = msg.ClaudeSessions
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

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.list.SetWidth(msg.Width)
	m.list.SetHeight(msg.Height)
	return m, nil
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if m.list.FilterState() == list.Filtering {
		return m, nil, false
	}
	if !key.Matches(msg, keys.Close) {
		m.pendingDeleteID = ""
	}
	switch {
	case key.Matches(msg, keys.ToggleDead):
		m.showDead = !m.showDead
		return m, m.rebuildItems(), true
	case key.Matches(msg, keys.New):
		return m, router.NavigateTo(router.SessionFormView), true
	case key.Matches(msg, keys.Todos):
		return m, router.NavigateTo(router.TodoListView), true
	case key.Matches(msg, keys.Select):
		if item, ok := m.list.SelectedItem().(sessionItem); ok {
			if item.session.IsAttached {
				return m, m.list.NewStatusMessage("already attached to this session"), true
			}
			if item.session.IsDead {
				return m, provider.ReviveSession(item.session.ID), true
			}
			return m, provider.ActivateSession(item.session.ID), true
		}
	case key.Matches(msg, keys.Close):
		item, ok := m.list.SelectedItem().(sessionItem)
		if !ok {
			return m, nil, false
		}
		if item.session.IsAttached {
			m.pendingDeleteID = ""
			return m, m.list.NewStatusMessage("cannot close attached session"), true
		}
		if m.pendingDeleteID == item.session.ID {
			m.pendingDeleteID = ""
			return m, provider.DeleteSession(item.session.ID), true
		}
		m.pendingDeleteID = item.session.ID
		return m, m.list.NewStatusMessage("press d again to close " + item.session.ID), true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
