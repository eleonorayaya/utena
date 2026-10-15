package provider

import tea "github.com/charmbracelet/bubbletea"

type Provider interface {
	Update(tea.Msg) (Provider, tea.Cmd)
}

type rootProvider struct {
	sessions   sessionsProvider
	workspaces workspacesProvider
	browser    browserProvider
}

func NewRootProvider(baseURL string) Provider {
	c := newClient(baseURL)
	return rootProvider{
		sessions:   newSessionsProvider(c),
		workspaces: newWorkspacesProvider(c),
		browser:    newBrowserProvider(c),
	}
}

func (p rootProvider) Update(msg tea.Msg) (Provider, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	p.sessions, cmd = p.sessions.Update(msg)
	cmds = append(cmds, cmd)
	p.workspaces, cmd = p.workspaces.Update(msg)
	cmds = append(cmds, cmd)
	p.browser, cmd = p.browser.Update(msg)
	cmds = append(cmds, cmd)
	return p, tea.Batch(cmds...)
}
