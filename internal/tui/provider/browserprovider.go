package provider

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/browser"
)

type BrowserStateUpdatedMsg struct {
	State browser.SessionsView
}

type FocusTabIntentMsg struct {
	SessionID uint
	WindowID  string
	TabID     string
}

func FocusTab(sessionID uint, windowID, tabID string) tea.Cmd {
	return func() tea.Msg { return FocusTabIntentMsg{SessionID: sessionID, WindowID: windowID, TabID: tabID} }
}

type EjectWindowIntentMsg struct {
	SessionID uint
	WindowID  string
}

func EjectWindow(sessionID uint, windowID string) tea.Cmd {
	return func() tea.Msg { return EjectWindowIntentMsg{SessionID: sessionID, WindowID: windowID} }
}

type MoveTabIntentMsg struct {
	browser.MoveTabRequest
}

func MoveTab(req browser.MoveTabRequest) tea.Cmd {
	return func() tea.Msg { return MoveTabIntentMsg{req} }
}

type TabMovedMsg struct {
	ToSessionID uint
}

func RequestBrowserState() tea.Cmd {
	return func() tea.Msg { return requestBrowserStateMsg{} }
}

type requestBrowserStateMsg struct{}

type browserStateLoadedMsg struct {
	state browser.SessionsView
}

type browserFocusedMsg struct{}

type browserEjectedMsg struct{}

type browserRequestFailedMsg struct {
	err error
}

type browserProvider struct {
	client *client
	state  browser.SessionsView
}

func newBrowserProvider(c *client) browserProvider {
	return browserProvider{client: c}
}

func (p browserProvider) emitState() tea.Cmd {
	state := p.state
	return func() tea.Msg { return BrowserStateUpdatedMsg{State: state} }
}

func (p browserProvider) Update(msg tea.Msg) (browserProvider, tea.Cmd) {
	switch msg := msg.(type) {
	case sessionsLoadedMsg, browserFocusedMsg, browserEjectedMsg, TabMovedMsg:
		return p, p.client.fetchBrowserState()
	case browserRequestFailedMsg:
		err := msg.err
		return p, tea.Batch(p.client.fetchBrowserState(), func() tea.Msg { return ErrMsg{err} })
	case browserStateLoadedMsg:
		p.state = msg.state
		return p, p.emitState()
	case requestBrowserStateMsg:
		return p, p.emitState()
	case FocusTabIntentMsg:
		return p, p.client.focusTab(msg.SessionID, msg.WindowID, msg.TabID)
	case EjectWindowIntentMsg:
		return p, p.client.ejectWindow(msg.SessionID, msg.WindowID)
	case MoveTabIntentMsg:
		return p, p.client.moveTab(msg.MoveTabRequest)
	}
	return p, nil
}
