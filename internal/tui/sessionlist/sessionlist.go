package sessionlist

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	ulist "github.com/eleonorayaya/utena/internal/tui/list"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
)

type Model struct {
	list            list.Model
	sessions        []session.Session
	claudeSessions  map[string][]claude.ClaudeSession
	pendingDeleteID uint
	pendingRepairID uint
	showBroken      bool
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
		if s.Status == session.StatusDeleted {
			continue
		}
		if s.Status == session.StatusBroken && !m.showBroken {
			continue
		}
		status := aggregateClaudeStatus(m.claudeSessions[s.TmuxSessionName])
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
	case provider.SessionSwitchedMsg:
		return m, tea.Quit
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
		m.pendingDeleteID = 0
	}
	if !key.Matches(msg, keys.Select) {
		m.pendingRepairID = 0
	}
	switch {
	case key.Matches(msg, keys.ToggleBroken):
		m.showBroken = !m.showBroken
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
			switch item.session.Status {
			case session.StatusCreating:
				return m, m.list.NewStatusMessage("session is still being created"), true
			case session.StatusBroken:
				if m.pendingRepairID == item.session.ID {
					m.pendingRepairID = 0
					id := item.session.ID
					return m, tea.Batch(
						provider.RepairSession(id),
						tea.Sequence(
							router.NavigateTo(router.SessionProgressView),
							sessionprogress.Start(id),
						),
					), true
				}
				errMsg := "broken"
				if item.session.Resources != nil {
					if e := item.session.Resources.FirstError(); e != "" {
						errMsg = e
					}
				}
				m.pendingRepairID = item.session.ID
				return m, m.list.NewStatusMessage(errMsg + " — press enter again to repair"), true
			default:
				return m, provider.ActivateSession(item.session.ID), true
			}
		}
	case key.Matches(msg, keys.Close):
		item, ok := m.list.SelectedItem().(sessionItem)
		if !ok {
			return m, nil, false
		}
		if item.session.IsAttached {
			m.pendingDeleteID = 0
			return m, m.list.NewStatusMessage("cannot close attached session"), true
		}
		if m.pendingDeleteID == item.session.ID {
			m.pendingDeleteID = 0
			return m, provider.DeleteSession(item.session.ID), true
		}
		m.pendingDeleteID = item.session.ID
		return m, m.list.NewStatusMessage(fmt.Sprintf("press d again to close %s", item.displayName())), true
	}
	return m, nil, false
}

func (m Model) View() string {
	return m.list.View()
}
