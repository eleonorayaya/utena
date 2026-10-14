package router

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

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
	case NavigateToMsg:
		return r.navigateTo(msg.Target)
	case BackMsg:
		return r.navigateBack()
	}

	cmd := r.views[r.activeView].Update(msg)
	return r, cmd
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
	return r.views[r.activeView].Keys()
}

func (r Router) Status() ui.Status {
	return r.views[r.activeView].Status()
}

func (r Router) ActiveView() View {
	return r.activeView
}

func (r Router) CapturesText() bool {
	return r.views[r.activeView].CapturesText()
}
