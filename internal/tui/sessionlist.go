package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
)

type activateSessionMsg struct {
	name string
}

type switchToNewSessionMsg struct{}

var (
	cursorStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	sessionStyle    = lipgloss.NewStyle()
	workspaceStyle  = lipgloss.NewStyle().Faint(true)
	emptyStateStyle = lipgloss.NewStyle().Faint(true)
)

type SessionListModel struct {
	sessions []session.Session
	cursor   int
	ready    bool
}

func NewSessionListModel() SessionListModel {
	return SessionListModel{}
}

func (m *SessionListModel) SetSessions(sessions []session.Session) {
	m.sessions = sessions
	m.ready = true
	if m.cursor >= len(sessions) {
		m.cursor = max(0, len(sessions)-1)
	}
}

func (m SessionListModel) Init() tea.Cmd {
	return nil
}

func (m SessionListModel) Update(msg tea.Msg) (SessionListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg:
		m.SetSessions(msg.sessions)
		return m, nil

	case tea.KeyMsg:
		if !m.ready || len(m.sessions) == 0 {
			switch msg.String() {
			case "n":
				return m, func() tea.Msg { return switchToNewSessionMsg{} }
			case "q", "esc":
				return m, tea.Quit
			}
			return m, nil
		}

		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			return m, func() tea.Msg {
				return activateSessionMsg{name: m.sessions[m.cursor].ID}
			}
		case "n":
			return m, func() tea.Msg { return switchToNewSessionMsg{} }
		case "q", "esc":
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m SessionListModel) View() string {
	if !m.ready {
		return ""
	}

	if len(m.sessions) == 0 {
		return emptyStateStyle.Render("No sessions found. Press 'n' to create one.")
	}

	var b strings.Builder
	for i, s := range m.sessions {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("❯ ")
		}

		line := sessionStyle.Render(s.ID)
		if s.WorkspaceID != "" {
			line += "  " + workspaceStyle.Render(s.WorkspaceID)
		}

		fmt.Fprintf(&b, "%s%s\n", cursor, line)
	}

	return b.String()
}
