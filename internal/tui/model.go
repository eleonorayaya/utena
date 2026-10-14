package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/sessionform"
	"github.com/eleonorayaya/utena/internal/tui/sessionlist"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
)

type ViewModel[T any] interface {
	Init() (T, tea.Cmd)
	OnWindowSizeMsg(tea.WindowSizeMsg) (T, tea.Cmd)
	OnKeyMsg(tea.KeyMsg) (T, tea.Cmd, bool)
}

var (
	_ ViewModel[sessionlist.Model]      = sessionlist.Model{}
	_ ViewModel[sessionform.Model]      = sessionform.Model{}
	_ ViewModel[sessionprogress.Model]  = sessionprogress.Model{}
	_ ViewModel[addworkspaceform.Model] = addworkspaceform.Model{}
)
