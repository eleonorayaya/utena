package debug

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

type Model struct {
	logPath   string
	daemonURL string
}

func New(logPath, daemonURL string) Model {
	return Model{logPath: logPath, daemonURL: daemonURL}
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		m, cmd, handled := m.OnKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, nil
}

func (m Model) OnWindowSizeMsg(_ tea.WindowSizeMsg) (Model, tea.Cmd) {
	return m, nil
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	if key.Matches(msg, keys.Back) {
		return m, func() tea.Msg { return BackMsg{} }, true
	}
	return m, nil, false
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(debugStyle.Render("Debug Info") + "\n\n")

	lines := []struct{ label, value string }{
		{"daemon", m.daemonURL},
		{"log", m.logPath},
	}

	for _, l := range lines {
		v := l.value
		if v == "" {
			v = "(not set)"
		}
		b.WriteString(fmt.Sprintf("  %s: %s\n", debugStyle.Render(l.label), v))
	}

	return b.String()
}
