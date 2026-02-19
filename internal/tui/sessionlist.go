package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type activateSessionMsg struct {
	name string
}

type switchToNewSessionMsg struct{}

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
	list           list.Model
	sessions       []session.Session
	claudeSessions map[string][]claude.ClaudeSession
}

func NewSessionListModel() SessionListModel {
	l := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	l.Title = "Sessions"
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{selectKey, newSessionKey}
	}
	return SessionListModel{list: l}
}

func (m *SessionListModel) SetSize(width, height int) {
	m.list.SetWidth(width)
	m.list.SetHeight(height)
}

func (m SessionListModel) Init() tea.Cmd {
	return nil
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
		if s.IsDead {
			continue
		}
		status := aggregateClaudeStatus(m.claudeSessions[s.ID])
		items = append(items, sessionItem{session: s, claudeStatus: status})
	}
	return m.list.SetItems(items)
}

func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		m.sessions = msg.sessions
		cmd := m.rebuildItems()
		return m, cmd

	case claudeSessionsLoadedMsg:
		m.claudeSessions = make(map[string][]claude.ClaudeSession)
		for _, cs := range msg.claudeSessions {
			m.claudeSessions[cs.SessionID] = append(m.claudeSessions[cs.SessionID], cs)
		}
		cmd := m.rebuildItems()
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
				if item.session.IsAttached {
					return m, m.list.NewStatusMessage("already attached to this session")
				}
				return m, func() tea.Msg {
					return activateSessionMsg{name: item.session.ID}
				}
			}
		case key.Matches(msg, newSessionKey):
			return m, func() tea.Msg { return switchToNewSessionMsg{} }
		}
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m SessionListModel) View() string {
	return m.list.View()
}
