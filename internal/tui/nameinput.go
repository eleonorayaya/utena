package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
)

type createSessionMsg struct {
	name        string
	workspaceID string
}

var errStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))

type NameInputModel struct {
	workspace workspace.Workspace
	input     textinput.Model
	err       string
}

func NewNameInputModel(ws workspace.Workspace) NameInputModel {
	ti := textinput.New()
	ti.Prompt = "Session name: "
	ti.Placeholder = defaultSessionName(ws.Name)
	ti.Focus()

	return NameInputModel{
		workspace: ws,
		input:     ti,
	}
}

func (m NameInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m NameInputModel) Update(msg tea.Msg) (NameInputModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			name := m.input.Value()
			if name == "" {
				name = defaultSessionName(m.workspace.Name)
			}
			if err := session.ValidateSessionName(name); err != nil {
				m.err = err.Error()
				return m, nil
			}
			m.err = ""
			return m, func() tea.Msg {
				return createSessionMsg{name: name, workspaceID: m.workspace.ID}
			}
		case "esc":
			return m, func() tea.Msg {
				return switchToSessionListMsg{}
			}
		}
	}

	m.err = ""
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m NameInputModel) View() string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("Workspace: %s\n\n", m.workspace.Name))
	b.WriteString(m.input.View())
	b.WriteString("\n")

	if m.err != "" {
		b.WriteString(errStyle.Render(m.err))
		b.WriteString("\n")
	}

	return b.String()
}

func defaultSessionName(workspaceName string) string {
	return strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
}
