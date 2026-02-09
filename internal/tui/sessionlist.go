package tui

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
)

type activateSessionMsg struct {
	name string
}

type switchToNewSessionMsg struct{}

type sessionItem struct {
	session session.Session
}

func (i sessionItem) Title() string       { return i.session.ID }
func (i sessionItem) Description() string { return i.session.WorkspaceID }
func (i sessionItem) FilterValue() string { return i.session.ID }

type SessionListModel struct {
	list list.Model
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

func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		items := make([]list.Item, len(msg.sessions))
		for i, s := range msg.sessions {
			items[i] = sessionItem{session: s}
		}
		cmd := m.list.SetItems(items)
		return m, cmd

	case tea.KeyMsg:
		if m.list.FilterState() == list.Filtering {
			break
		}
		switch {
		case key.Matches(msg, selectKey):
			if item, ok := m.list.SelectedItem().(sessionItem); ok {
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
