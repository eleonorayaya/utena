package sessionprogress

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func titleStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }
func pendingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.StatusPending)
}
func failedStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.Error) }
func warningStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.AccentLavender)
}

type tickMsg time.Time

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

type Model struct {
	sessionID uint
	session   *session.Session
	spinner   spinner.Model
	done      bool
	err       error
	warning   string
	width     int
	height    int
}

func New() Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(theme.Current.Primary)
	return Model{spinner: sp}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.session = nil
	m.done = false
	m.err = nil
	m.warning = ""
	return m, m.spinner.Tick
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func tick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil

	case StartMsg:
		m.sessionID = msg.SessionID
		m.session = nil
		m.done = false
		m.err = nil
		m.warning = ""
		return m, tea.Batch(provider.PollSession(m.sessionID), tick(), m.spinner.Tick)

	case spinner.TickMsg:
		if !m.done {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tickMsg:
		if m.done || m.sessionID == 0 {
			return m, nil
		}
		return m, tea.Batch(provider.PollSession(m.sessionID), tick())

	case provider.SessionSwitchedMsg:
		return m, tea.Quit

	case provider.SessionPolledMsg:
		s := msg.Session
		if s.ID != m.sessionID {
			return m, nil
		}
		m.session = &s
		switch s.Status {
		case session.StatusActive:
			if s.StatusError != "" {
				m.done = true
				m.warning = s.StatusError
				return m, nil
			}
			m.done = true
			return m, provider.ActivateSession(s.ID)
		case session.StatusBroken:
			m.done = true
			errMsg := "session creation failed"
			if s.StatusError != "" {
				errMsg = s.StatusError
			}
			m.err = fmt.Errorf("%s", errMsg)
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		if m.done {
			if key.Matches(msg, keys.Back) {
				if m.warning != "" {
					return m, tea.Batch(router.Back(), provider.ActivateSession(m.sessionID))
				}
				return m, router.Back()
			}
			if (m.err != nil || m.warning != "") && key.Matches(msg, keys.Repair) {
				m.done = false
				m.err = nil
				m.warning = ""
				return m, tea.Batch(
					provider.RepairSession(m.sessionID),
					provider.PollSession(m.sessionID),
					tick(),
				)
			}
		}
	}

	return m, nil
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.SetSize(msg.Width, msg.Height)
	return m, nil
}

func (m Model) OnKeyMsg(_ tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
}

func (m Model) View() string {
	var b strings.Builder

	name := fmt.Sprintf("%d", m.sessionID)
	if m.session != nil && m.session.Name != "" {
		name = m.session.Name
	}
	b.WriteString(titleStyle().Render("Creating session: " + name))
	b.WriteString("\n\n")

	if m.session == nil {
		b.WriteString(m.spinner.View() + pendingStyle().Render(" Loading..."))
		b.WriteString("\n")
	} else if m.session.Status == session.StatusCreating {
		b.WriteString(m.spinner.View() + pendingStyle().Render(" Setting up..."))
		b.WriteString("\n")
	}

	if m.warning != "" {
		b.WriteString("\n")
		b.WriteString(warningStyle().Render("[!] " + m.warning))
		b.WriteString("\n")
		b.WriteString(pendingStyle().Render("  Session is active. Press esc to open it or r to retry setup."))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(failedStyle().Render("Error: " + m.err.Error()))
		b.WriteString("\n")
	}

	return b.String()
}
