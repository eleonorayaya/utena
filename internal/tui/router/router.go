package router

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type routerKeyMap struct {
	Debug key.Binding
}

func (k routerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Debug}
}

func (k routerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Debug}}
}

var routerKeys = routerKeyMap{
	Debug: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "debug")),
}

type Router struct {
	views      map[View]ViewEntry
	activeView View
	viewStack  []View
}

func New(views map[View]ViewEntry, initialView View) Router {
	return Router{
		views:      views,
		activeView: initialView,
	}
}

func (r Router) Init() (Router, tea.Cmd) {
	cmd := r.views[r.activeView].Init()
	return r, cmd
}

func (r Router) Update(msg tea.Msg) (Router, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return r.OnWindowSizeMsg(msg)
	case tea.KeyMsg:
		r, cmd, handled := r.OnKeyMsg(msg)
		if handled {
			return r, cmd
		}
	case NavigateToMsg:
		return r.navigateTo(msg.Target)
	case BackMsg:
		return r.navigateBack()
	}

	cmd := r.views[r.activeView].Update(msg)
	return r, cmd
}

func (r Router) OnKeyMsg(msg tea.KeyMsg) (Router, tea.Cmd, bool) {
	if key.Matches(msg, routerKeys.Debug) && r.activeView != DebugView {
		r.viewStack = append(r.viewStack, r.activeView)
		r.activeView = DebugView
		return r, nil, true
	}
	return r, nil, false
}

func (r Router) navigateTo(target View) (Router, tea.Cmd) {
	r.viewStack = append(r.viewStack, r.activeView)
	r.activeView = target
	return r.Init()
}

func (r Router) navigateBack() (Router, tea.Cmd) {
	if len(r.viewStack) == 0 {
		return r, nil
	}
	r.activeView = r.viewStack[len(r.viewStack)-1]
	r.viewStack = r.viewStack[:len(r.viewStack)-1]
	return r.Init()
}

func (r Router) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Router, tea.Cmd) {
	var cmds []tea.Cmd
	for _, v := range r.views {
		cmds = append(cmds, v.Update(msg))
	}
	return r, tea.Batch(cmds...)
}

func (r Router) View() string {
	return r.views[r.activeView].View()
}

func (r Router) Keys() help.KeyMap {
	return mergedKeyMap{keymaps: []help.KeyMap{routerKeys, r.views[r.activeView].Keys()}}
}

type mergedKeyMap struct {
	keymaps []help.KeyMap
}

func (m mergedKeyMap) ShortHelp() []key.Binding {
	var bindings []key.Binding
	for _, km := range m.keymaps {
		bindings = append(bindings, km.ShortHelp()...)
	}
	return bindings
}

func (m mergedKeyMap) FullHelp() [][]key.Binding {
	var groups [][]key.Binding
	for _, km := range m.keymaps {
		groups = append(groups, km.FullHelp()...)
	}
	return groups
}
