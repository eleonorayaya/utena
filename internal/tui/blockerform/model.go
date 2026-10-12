package blockerform

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

const (
	kindField = iota
	noteField
	urlField
	fieldCount
)

type Model struct {
	sessionID uint
	kindIdx   int
	focus     int
	note      textinput.Model
	url       textinput.Model
	err       string
	width     int
	height    int
}

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func New() Model {
	note := textinput.New()
	note.Prompt = "Note: "
	note.Placeholder = "optional — what are you waiting on?"

	url := textinput.New()
	url.Prompt = "URL:  "
	url.Placeholder = "optional — PR or Slack link"

	return Model{note: note, url: url}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.sessionID = 0
	m.reset()
	return m, nil
}

func (m *Model) reset() {
	m.kindIdx = 0
	m.focus = kindField
	m.err = ""
	m.note.SetValue("")
	m.url.SetValue("")
	m.note.Blur()
	m.url.Blur()
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func (m Model) kind() session.BlockerKind {
	return session.BlockerKinds[m.kindIdx]
}

func (m *Model) setFocus(f int) {
	m.focus = (f + fieldCount) % fieldCount
	m.note.Blur()
	m.url.Blur()
	switch m.focus {
	case noteField:
		m.note.Focus()
	case urlField:
		m.url.Focus()
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case StartMsg:
		m.sessionID = msg.SessionID
		m.reset()
		return m, nil
	case provider.BlockerAddedMsg:
		return m, router.Back()
	case provider.ErrMsg:
		m.err = msg.Err.Error()
		return m, nil
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Back):
		return m, router.Back()
	case key.Matches(msg, keys.Submit):
		if m.sessionID == 0 {
			return m, nil
		}
		return m, provider.AddBlocker(m.sessionID, m.kind(),
			strings.TrimSpace(m.note.Value()), strings.TrimSpace(m.url.Value()))
	case key.Matches(msg, keys.Next):
		m.setFocus(m.focus + 1)
		return m, nil
	case key.Matches(msg, keys.Prev):
		m.setFocus(m.focus - 1)
		return m, nil
	}

	if m.focus == kindField {
		n := len(session.BlockerKinds)
		switch {
		case key.Matches(msg, keys.KindNext):
			m.kindIdx = (m.kindIdx + 1) % n
		case key.Matches(msg, keys.KindPrev):
			m.kindIdx = (m.kindIdx - 1 + n) % n
		}
		return m, nil
	}

	var cmd tea.Cmd
	if m.focus == noteField {
		m.note, cmd = m.note.Update(msg)
	} else {
		m.url, cmd = m.url.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("Add blocker"))
	b.WriteString("\n\n")

	var kinds []string
	for i, k := range session.BlockerKinds {
		style := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
		if i == m.kindIdx {
			style = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
			if m.focus == kindField {
				style = style.Underline(true)
			}
		}
		kinds = append(kinds, style.Render(string(k)))
	}
	b.WriteString("Kind: " + strings.Join(kinds, "  ") + "\n")
	b.WriteString(m.note.View() + "\n")
	b.WriteString(m.url.View() + "\n")

	if m.err != "" {
		b.WriteString("\n" + lipgloss.NewStyle().Foreground(theme.Current.Error).Render(m.err) + "\n")
	}
	return b.String()
}
