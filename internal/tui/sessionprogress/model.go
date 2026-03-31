package sessionprogress

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func titleStyle() lipgloss.Style { return lipgloss.NewStyle().Bold(true) }
func readyStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.StatusReady) }
func pendingStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(theme.Current.StatusPending)
}
func activeStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.StatusActive) }
func failedStyle() lipgloss.Style { return lipgloss.NewStyle().Foreground(theme.Current.Error) }

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
	done      bool
	err       error
	width     int
	height    int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.session = nil
	m.done = false
	m.err = nil
	return m, nil
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
		return m, tea.Batch(provider.PollSession(m.sessionID), tick())

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
		case session.StatusReady:
			m.done = true
			return m, provider.ActivateSession(s.ID)
		case session.StatusBroken:
			m.done = true
			errMsg := "session creation failed"
			if s.Resources != nil {
				if e := s.Resources.FirstError(); e != "" {
					errMsg = e
				}
			}
			m.err = fmt.Errorf("%s", errMsg)
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		if m.done {
			if key.Matches(msg, keys.Back) {
				return m, router.Back()
			}
			if m.err != nil && key.Matches(msg, keys.Repair) {
				m.done = false
				m.err = nil
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

	if m.session != nil && m.session.Resources != nil {
		res := m.session.Resources
		if res.Branch != nil {
			b.WriteString(resourceLine("Branch", res.Branch))
			b.WriteString("\n")
		}
		if res.Worktree != nil {
			b.WriteString(resourceLine("Worktree", res.Worktree))
			b.WriteString("\n")
		}
		if res.WorktreeInit != nil {
			b.WriteString(resourceLine("Setup", res.WorktreeInit))
			b.WriteString("\n")
		}
		if res.Tmux != nil {
			b.WriteString(resourceLine("Tmux", res.Tmux))
			b.WriteString("\n")
		}
	} else {
		b.WriteString(pendingStyle().Render("  Loading..."))
		b.WriteString("\n")
	}

	if m.err != nil {
		b.WriteString("\n")
		b.WriteString(failedStyle().Render("Error: " + m.err.Error()))
		b.WriteString("\n")
	}

	return b.String()
}

func resourceLine(name string, rs *session.ResourceState) string {
	icon := statusIcon(rs.Status)
	style := statusStyle(rs.Status)
	line := fmt.Sprintf("  %s %s", icon, style.Render(name))
	if rs.Error != "" {
		line += " " + failedStyle().Render("— "+rs.Error)
	}
	return line
}

func statusIcon(s session.ResourceStatus) string {
	switch s {
	case session.ResourceReady:
		return readyStyle().Render("✓")
	case session.ResourceCreating:
		return activeStyle().Render("●")
	case session.ResourcePending:
		return pendingStyle().Render("○")
	case session.ResourceFailed:
		return failedStyle().Render("✗")
	case session.ResourceRemoved:
		return failedStyle().Render("–")
	default:
		return " "
	}
}

func statusStyle(s session.ResourceStatus) lipgloss.Style {
	switch s {
	case session.ResourceReady:
		return readyStyle()
	case session.ResourceCreating:
		return activeStyle()
	case session.ResourceFailed, session.ResourceRemoved:
		return failedStyle()
	default:
		return pendingStyle()
	}
}
