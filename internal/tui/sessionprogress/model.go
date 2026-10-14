package sessionprogress

import (
	"fmt"
	"slices"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type tickMsg time.Time

type StartMsg struct {
	SessionID uint
	Repairing bool
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func StartRepair(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID, Repairing: true} }
}

type Model struct {
	sessionID uint
	repairing bool
	session   *session.Session
	spinner   spinner.Model
	done      bool
	err       string
	warning   string
	width     int
	height    int
}

func New() Model {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	return Model{spinner: sp}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.session = nil
	m.done = false
	m.err = ""
	m.warning = ""
	return m, m.spinner.Tick
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m Model) Keys() help.KeyMap {
	k := keys
	k.Repair.SetEnabled(m.done && (m.err != "" || m.warning != ""))
	if m.warning != "" {
		k.Back.SetHelp("esc", "open session")
	}
	return k
}

func (m Model) Status() ui.Status {
	switch {
	case m.err != "":
		return ui.Status{Text: "✕ " + m.err, Kind: ui.StatusError}
	case m.warning != "":
		return ui.Status{Text: "⚠ " + m.warning, Kind: ui.StatusWarning}
	}
	return ui.Status{}
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
		m.repairing = msg.Repairing
		m.session = nil
		m.done = false
		m.err = ""
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
			m.done = true
			if s.StatusError != "" {
				m.warning = s.StatusError
				return m, nil
			}
			return m, provider.ActivateSession(s.ID)
		case session.StatusBroken:
			m.done = true
			m.err = "session setup failed"
			if s.StatusError != "" {
				m.err = s.StatusError
			}
			return m, nil
		}
		return m, nil

	case tea.KeyMsg:
		if !m.done {
			return m, nil
		}
		if key.Matches(msg, keys.Back) {
			if m.warning != "" {
				return m, tea.Batch(router.Back(), provider.ActivateSession(m.sessionID))
			}
			return m, router.Back()
		}
		if (m.err != "" || m.warning != "") && key.Matches(msg, keys.Repair) {
			m.done = false
			m.repairing = true
			m.err = ""
			m.warning = ""
			return m, tea.Batch(provider.RepairSession(m.sessionID), provider.PollSession(m.sessionID), tick(), m.spinner.Tick)
		}
	}

	return m, nil
}

func (m Model) title() string {
	name := fmt.Sprintf("session %d", m.sessionID)
	if m.session != nil && m.session.Name != "" {
		name = m.session.Name
	}
	if m.repairing {
		return "Repairing " + name
	}
	return "Creating " + name
}

func stepState(s session.SetupStepStatus) ui.StepState {
	switch s {
	case session.SetupStepRunning:
		return ui.StepRunning
	case session.SetupStepDone:
		return ui.StepDone
	case session.SetupStepFailed:
		return ui.StepFailed
	default:
		return ui.StepPending
	}
}

func (m Model) steps() []ui.Step {
	if m.session == nil || len(m.session.SetupSteps) == 0 {
		if m.done {
			return nil
		}
		return []ui.Step{{Label: "Setting up", State: ui.StepRunning}}
	}
	src := slices.Clone(m.session.SetupSteps)
	slices.SortStableFunc(src, func(a, b session.SessionSetupStep) int { return a.Position - b.Position })
	out := make([]ui.Step, len(src))
	for i, s := range src {
		out[i] = ui.Step{Label: s.Label, State: stepState(s.Status), Error: s.ErrorMsg}
	}
	return out
}

func (m Model) View() string {
	return ui.FitHeight(ui.Clamp(ui.FormTitle(m.title()), m.width)+"\n\n"+ui.StepList(m.steps(), m.spinner.View(), m.width), m.height)
}
