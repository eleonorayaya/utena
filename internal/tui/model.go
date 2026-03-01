package tui

import tea "github.com/charmbracelet/bubbletea"

type ViewModel[T any] interface {
	OnWindowSizeMsg(tea.WindowSizeMsg) (T, tea.Cmd)
	OnKeyMsg(tea.KeyMsg) (T, tea.Cmd, bool)
}

var (
	_ ViewModel[Router]               = Router{}
	_ ViewModel[SessionListModel]     = SessionListModel{}
	_ ViewModel[SessionFormModel]     = SessionFormModel{}
	_ ViewModel[TodoListModel]        = TodoListModel{}
	_ ViewModel[TodoFormModel]        = TodoFormModel{}
	_ ViewModel[DebugModel]           = DebugModel{}
	_ ViewModel[WorkspacePickerModel] = WorkspacePickerModel{}
	_ ViewModel[BranchPickerModel]    = BranchPickerModel{}
	_ ViewModel[FilePickerModel]      = FilePickerModel{}
)
