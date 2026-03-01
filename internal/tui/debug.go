package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var debugStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))


type DebugModel struct {
	logPath string
}

func NewDebugModel(logPath string) DebugModel {
	return DebugModel{logPath: logPath}
}

func (m DebugModel) Keys() help.KeyMap {
	return debugKeys
}

func (m DebugModel) Update(msg tea.Msg) (DebugModel, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		m, cmd, handled := m.onKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m DebugModel) onKeyMsg(msg tea.KeyMsg) (DebugModel, tea.Cmd, bool) {
	if key.Matches(msg, debugKeys.Back) {
		return m, func() tea.Msg { return navigateMsg{target: backView} }, true
	}
	return m, nil, false
}

func (m DebugModel) View() string {
	var b strings.Builder
	b.WriteString(debugStyle.Render("Debug Info") + "\n\n")

	lines := []struct{ label, value string }{
		{"daemon", baseURL},
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
