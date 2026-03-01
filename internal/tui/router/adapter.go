package router

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
)

type ViewEntry interface {
	Init() tea.Cmd
	Update(tea.Msg) tea.Cmd
	View() string
	Keys() help.KeyMap
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
