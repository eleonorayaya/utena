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

func RequestBrowserState() tea.Cmd {
	return func() tea.Msg { return requestBrowserStateMsg{} }
}

type requestBrowserStateMsg struct{}

type browserStateLoadedMsg struct {
	state browser.SessionsView
}

type browserFocusedMsg struct{}

type browserFocusFailedMsg struct {
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
	case sessionsLoadedMsg, browserFocusedMsg:
		return p, p.client.fetchBrowserState()
	case browserFocusFailedMsg:
		err := msg.err
		return p, tea.Batch(p.client.fetchBrowserState(), func() tea.Msg { return ErrMsg{err} })
	case browserStateLoadedMsg:
		p.state = msg.state
		return p, p.emitState()
	case requestBrowserStateMsg:
		return p, p.emitState()
	case FocusTabIntentMsg:
		return p, p.client.focusTab(msg.SessionID, msg.WindowID, msg.TabID)
	}
	return p, nil
}
