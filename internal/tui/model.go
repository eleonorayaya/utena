package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/debug"
	"github.com/eleonorayaya/utena/internal/tui/filepicker"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionform"
	"github.com/eleonorayaya/utena/internal/tui/sessionlist"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/todoform"
	"github.com/eleonorayaya/utena/internal/tui/todolist"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
)

type ViewModel[T any] interface {
	Init() (T, tea.Cmd)
	OnWindowSizeMsg(tea.WindowSizeMsg) (T, tea.Cmd)
	OnKeyMsg(tea.KeyMsg) (T, tea.Cmd, bool)
}

var (
	_ ViewModel[router.Router]          = router.Router{}
	_ ViewModel[sessionlist.Model]      = sessionlist.Model{}
	_ ViewModel[sessionform.Model]      = sessionform.Model{}
	_ ViewModel[sessionprogress.Model]  = sessionprogress.Model{}
	_ ViewModel[todolist.Model]         = todolist.Model{}
	_ ViewModel[todoform.Model]         = todoform.Model{}
	_ ViewModel[debug.Model]            = debug.Model{}
	_ ViewModel[workspacepicker.Model]  = workspacepicker.Model{}
	_ ViewModel[branchpicker.Model]     = branchpicker.Model{}
	_ ViewModel[filepicker.Model]       = filepicker.Model{}
	_ ViewModel[addworkspaceform.Model] = addworkspaceform.Model{}
)
