package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type switchToNameInputMsg struct {
	workspace workspace.Workspace
}

type switchToSessionListMsg struct{}

type NewSessionModel struct {
	workspaces []workspace.Workspace
	cursor     int
	ready      bool
}

func NewNewSessionModel() NewSessionModel {
	return NewSessionModel{}
}

func (m NewSessionModel) Init() tea.Cmd {
	return nil
}

func (m NewSessionModel) Update(msg tea.Msg) (NewSessionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacesLoadedMsg:
		m.workspaces = msg.workspaces
		m.cursor = 0
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if m.cursor < len(m.workspaces)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter":
			if len(m.workspaces) > 0 {
				return m, func() tea.Msg {
					return switchToNameInputMsg{workspace: m.workspaces[m.cursor]}
				}
			}
		case "esc":
			return m, func() tea.Msg {
				return switchToSessionListMsg{}
			}
		}
	}

	return m, nil
}

var dimStyle = lipgloss.NewStyle().Faint(true)

func (m NewSessionModel) View() string {
	var b strings.Builder

	b.WriteString("Select workspace:\n\n")

	if !m.ready {
		return b.String()
	}

	if len(m.workspaces) == 0 {
		b.WriteString("No workspaces found.\n")
	} else {
		for i, ws := range m.workspaces {
			cursor := "  "
			if i == m.cursor {
				cursor = "❯ "
			}
			path := abbreviatePath(ws.Path)
			b.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, ws.Name, dimStyle.Render(path)))
		}
	}

	return b.String()
}

func abbreviatePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}
