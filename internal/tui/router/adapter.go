package router

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type ViewEntry interface {
	Init() tea.Cmd
	Update(tea.Msg) tea.Cmd
	View() string
	Keys() help.KeyMap
	Status() ui.Status
	CapturesText() bool
}

type ViewAdapter[T interface {
	Init() (T, tea.Cmd)
	Update(tea.Msg) (T, tea.Cmd)
	View() string
	Keys() help.KeyMap
}] struct {
	Model T
}

func (a *ViewAdapter[T]) Init() tea.Cmd {
	m, cmd := a.Model.Init()
	a.Model = m
	return cmd
}

func (a *ViewAdapter[T]) Update(msg tea.Msg) tea.Cmd {
	m, cmd := a.Model.Update(msg)
	a.Model = m
	return cmd
}

func (a *ViewAdapter[T]) View() string {
	return a.Model.View()
}

func (a *ViewAdapter[T]) Keys() help.KeyMap {
	return a.Model.Keys()
}

func (a *ViewAdapter[T]) Status() ui.Status {
	if sr, ok := any(a.Model).(ui.StatusReporter); ok {
		return sr.Status()
	}
	return ui.Status{}
}

func (a *ViewAdapter[T]) CapturesText() bool {
	if tc, ok := any(a.Model).(ui.TextCapturer); ok {
		return tc.CapturesText()
	}
	return false
}
