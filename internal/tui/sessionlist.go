package tui

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type sessionItem struct {
	session      session.Session
	claudeStatus string
}

func (i sessionItem) Title() string {
	title := i.session.ID
	if i.session.IsAttached {
		title += " (attached)"
	}
	if i.claudeStatus != "" {
		title += " " + i.claudeStatus
	}
	return title
}

func (i sessionItem) Description() string {
	name := i.session.WorkspaceName
	if name == "" {
		name = "no workspace"
	}
	if !i.session.LastUsedAt.IsZero() {
		return name + " · " + timeAgo(i.session.LastUsedAt)
	}
	return name
}

func (i sessionItem) FilterValue() string { return i.session.ID }

type SessionListModel struct {
	list            list.Model
	sessions        []session.Session
	claudeSessions  map[string][]claude.ClaudeSession
	pendingDeleteID string
}

func NewSessionListModel() SessionListModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Sessions"
	l.SetShowHelp(false)
	return SessionListModel{list: l}
}

func (m *SessionListModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m SessionListModel) Init() tea.Cmd {
	return func() tea.Msg { return requestSessionsStateMsg{} }
}

func (m SessionListModel) Filtering() bool {
	return m.list.FilterState() == list.Filtering
}

func (m SessionListModel) Keys() help.KeyMap {
	return sessionListKeys
}

func aggregateClaudeStatus(sessions []claude.ClaudeSession) string {
	if len(sessions) == 0 {
		return ""
	}

	hasNeedsAttention := false
	hasWorking := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
		case claude.StatusWorking:
			hasWorking = true
		}
	}

	if hasNeedsAttention {
		return "[needs attention]"
	}
	if hasWorking {
		return "[working]"
	}
	return "[done]"
}

func (m *SessionListModel) rebuildItems() tea.Cmd {
	var items []list.Item
	for _, s := range m.sessions {
		if s.IsDead || s.IsDeleted {
			continue
		}
		status := aggregateClaudeStatus(m.claudeSessions[s.ID])
		items = append(items, sessionItem{session: s, claudeStatus: status})
	}
	return m.list.SetItems(items)
}

func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsStateUpdatedMsg:
		m.sessions = msg.sessions
		m.claudeSessions = msg.claudeSessions
		cmd := m.rebuildItems()
		return m, cmd

	case errMsg:
		return m, m.list.NewStatusMessage(msg.err.Error())

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		if !key.Matches(msg, sessionListKeys.Close) {
			m.pendingDeleteID = ""
		}
		switch {
		case key.Matches(msg, sessionListKeys.New):
			return m, func() tea.Msg { return openSessionFormMsg{} }
		case key.Matches(msg, sessionListKeys.Todos):
			return m, func() tea.Msg { return openTodosViewMsg{} }
		case key.Matches(msg, sessionListKeys.Select):
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
				if item.session.IsAttached {
					return m, m.list.NewStatusMessage("already attached to this session")
				}
				return m, func() tea.Msg {
					return activateSessionMsg{name: item.session.ID}
				}
			}
		case key.Matches(msg, sessionListKeys.Close):
			item, ok := m.list.SelectedItem().(sessionItem)
			if !ok {
				break
			}
			if item.session.IsAttached {
				m.pendingDeleteID = ""
				return m, m.list.NewStatusMessage("cannot close attached session")
			}
			if m.pendingDeleteID == item.session.ID {
				m.pendingDeleteID = ""
				return m, func() tea.Msg {
					return deleteSessionIntentMsg{id: item.session.ID}
				}
			}
			m.pendingDeleteID = item.session.ID
			return m, m.list.NewStatusMessage("press d again to close " + item.session.ID)
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m SessionListModel) View() string {
	return m.list.View()
}
