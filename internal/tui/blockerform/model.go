package blockerform

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	kindField = iota
	noteField
	urlField
	fieldCount
)

var labels = [fieldCount]string{"Kind", "Note", "URL"}

type Model struct {
	sessionID  uint
	form       ui.Form
	kindChoice ui.Choice
	note       textinput.Model
	url        textinput.Model
	status     ui.Status
	submitting bool
	width      int
	height     int
}

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func kindOptions() []string {
	out := make([]string, len(session.BlockerKinds))
	for i, k := range session.BlockerKinds {
		out[i] = string(k)
	}
	return out
}

func New() Model {
	return Model{
		form:       ui.NewForm("Add blocker", fieldCount),
		kindChoice: ui.Choice{Options: kindOptions()},
		note:       ui.NewInput("what are you waiting on?"),
		url:        ui.NewInput("PR or Slack link"),
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.sessionID = 0
	m.reset()
	return m, nil
}

func (m *Model) reset() {
	m.form = m.form.Focus(kindField)
	m.kindChoice.Selected = 0
	m.status = ui.Status{}
	m.submitting = false
	m.note.SetValue("")
	m.url.SetValue("")
	m.note.Blur()
	m.url.Blur()
}

func (m Model) Keys() help.KeyMap {
	k := keys
	k.Kind.SetEnabled(m.form.Focused() == kindField)
	return k
}

func (m Model) Status() ui.Status {
	return m.status
}

func (m Model) kind() session.BlockerKind {
	return session.BlockerKinds[m.kindChoice.Selected]
}

func (m *Model) setFocus(f int) tea.Cmd {
	m.form = m.form.Focus(f)
	m.note.Blur()
	m.url.Blur()
	switch m.form.Focused() {
	case noteField:
		return m.note.Focus()
	case urlField:
		return m.url.Focus()
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.note.Width = ui.InputWidth(msg.Width, labels[:]...)
		m.url.Width = m.note.Width
		return m, nil
	case StartMsg:
		m.sessionID = msg.SessionID
		m.reset()
		return m, nil
	case provider.BlockerAddedMsg:
		m.submitting = false
		return m, router.Back()
	case provider.ErrMsg:
		m.submitting = false
		m.status = ui.Status{Text: msg.Err.Error(), Kind: ui.StatusError}
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
		if m.sessionID == 0 || m.submitting {
			return m, nil
		}
		m.submitting = true
		m.status = ui.Status{}
		return m, provider.AddBlocker(m.sessionID, m.kind(),
			strings.TrimSpace(m.note.Value()), strings.TrimSpace(m.url.Value()))
	case key.Matches(msg, keys.Next):
		return m, m.setFocus(m.form.Focused() + 1)
	case key.Matches(msg, keys.Prev):
		return m, m.setFocus(m.form.Focused() - 1)
	}

	var cmd tea.Cmd
	switch m.form.Focused() {
	case kindField:
		m.kindChoice, _ = m.kindChoice.Update(msg)
	case noteField:
		m.note, cmd = m.note.Update(msg)
	case urlField:
		m.url, cmd = m.url.Update(msg)
	}
	return m, cmd
}

func (m Model) View() string {
	fields := []ui.Field{
		{Label: labels[kindField], Value: m.kindChoice.View(m.form.Focused() == kindField)},
		{Label: labels[noteField], Value: m.note.View()},
		{Label: labels[urlField], Value: m.url.View()},
	}
	return ui.FitHeight(m.form.View(fields, m.width), m.height)
}

func (m Model) CapturesText() bool {
	return m.form.Focused() != kindField
}
