package addworkspaceform

import (
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/branchfield"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
)

const (
	fieldWorkspace = iota
	fieldBranch
	fieldMode
	fieldCount
)

type overlay int

const (
	overlayNone overlay = iota
	overlayWorkspace
	overlayBranch
)

type Model struct {
	form            ui.Form
	overlay         overlay
	workspacePicker workspacepicker.Model
	branchPicker    branchpicker.Model
	sessionID       uint
	row             branchfield.Row
	hasRow          bool
	mode            ui.Choice
	status          ui.Status
	submitting      bool
	width, height   int
}

func New() Model {
	return Model{
		form:            ui.NewForm("Add workspace", fieldCount),
		workspacePicker: workspacepicker.New("Workspace to add", false).WithoutAddDir(),
		branchPicker:    branchpicker.New(),
		mode:            branchfield.NewModeChoice(),
	}
}

type StartMsg struct {
	SessionID uint
}

func Start(sessionID uint) tea.Cmd {
	return func() tea.Msg { return StartMsg{SessionID: sessionID} }
}

func (m Model) reset() Model {
	m.form = m.form.Focus(fieldMode)
	m.row, m.hasRow = branchfield.Row{}, false
	m.mode = branchfield.NewModeChoice()
	m.status = ui.Status{}
	m.submitting = false
	m.overlay = overlayWorkspace
	m.workspacePicker = m.workspacePicker.Open(nil)
	return m
}

func (m Model) Init() (Model, tea.Cmd) {
	m = m.reset()
	m.sessionID = 0
	return m, provider.FetchWorkspaces()
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.workspacePicker.SetSize(width, height)
	m.branchPicker.SetSize(width, height)
}

func (m Model) Keys() help.KeyMap {
	switch m.overlay {
	case overlayWorkspace:
		return m.workspacePicker.Keys()
	case overlayBranch:
		return m.branchPicker.Keys()
	}
	k := formKeys
	onMode := m.form.Focused() == fieldMode
	k.Add.SetEnabled(onMode)
	k.Edit.SetEnabled(!onMode)
	return k
}

func (m Model) Status() ui.Status {
	if m.overlay == overlayBranch {
		if s := m.branchPicker.Status(); !s.Empty() {
			return s
		}
	}
	return m.status
}

func (m Model) CapturesText() bool {
	switch m.overlay {
	case overlayWorkspace:
		return m.workspacePicker.CapturesText()
	case overlayBranch:
		return m.branchPicker.CapturesText()
	}
	return m.hasRow && m.row.Editing
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case StartMsg:
		m = m.reset()
		m.sessionID = msg.SessionID
		return m, provider.RequestWorkspacesState()
	case provider.WorkspacesStateUpdatedMsg:
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	case provider.BranchesLoadedMsg:
		if m.hasRow && m.row.Workspace.ID == msg.WorkspaceID {
			m.row = m.row.Loaded(msg)
		}
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchesFetchedMsg:
		m.row = m.row.Fetched(msg)
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchExistsCheckedMsg:
		m.row = m.row.Checked(msg)
		return m, nil
	case provider.ErrMsg:
		m.submitting = false
		m.status = ui.Status{Text: msg.Err.Error(), Kind: ui.StatusError}
		return m, nil
	case provider.WorkspaceAddedToSessionMsg:
		m.submitting = false
		return m, tea.Sequence(router.NavigateTo(router.SessionProgressView), sessionprogress.Start(msg.SessionID))
	}

	switch m.overlay {
	case overlayWorkspace:
		return m.updateWorkspacePicker(msg)
	case overlayBranch:
		return m.updateBranchPicker(msg)
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		return m.onPageKey(keyMsg)
	}
	return m, nil
}

func (m Model) updateWorkspacePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacepicker.SelectedMsg:
		ws := msg.Workspaces[0]
		if !ws.IsGitRepo {
			m.status = ui.Status{Text: ws.Name + " is not a git repository", Kind: ui.StatusError}
			return m, nil
		}
		m.overlay = overlayNone
		m.status = ui.Status{}
		m.form = m.form.Focus(fieldMode)
		if m.hasRow && m.row.Workspace.ID == ws.ID {
			return m, nil
		}
		m.row, m.hasRow = branchfield.NewRow(ws), true
		return m, provider.RequestBranches(ws.ID)
	case workspacepicker.ClosedMsg:
		m.overlay = overlayNone
		m.status = ui.Status{}
		return m, nil
	}
	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		m.row = m.row.Pick(msg.Branch)
		m.overlay = overlayNone
		m.form = m.form.Focus(fieldBranch)
		return m, nil
	case branchpicker.FetchRequestedMsg:
		return m, provider.FetchOriginBranches(m.branchPicker.WorkspaceID())
	case branchpicker.ManualEntryRequestedMsg:
		m.overlay = overlayNone
		m.form = m.form.Focus(fieldBranch)
		var cmd tea.Cmd
		m.row, cmd = m.row.StartManual()
		return m, cmd
	case branchpicker.ClosedMsg:
		m.overlay = overlayNone
		return m, nil
	}
	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m Model) openWorkspacePicker() Model {
	var ids []uint
	if m.hasRow {
		ids = []uint{m.row.Workspace.ID}
	}
	m.workspacePicker = m.workspacePicker.Open(ids)
	m.overlay = overlayWorkspace
	return m
}

func (m Model) onPageKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.hasRow && m.row.Editing {
		var cmd tea.Cmd
		m.row, cmd = m.row.UpdateManual(msg)
		return m, cmd
	}
	field := m.form.Focused()
	switch {
	case key.Matches(msg, formKeys.Cancel):
		return m, router.Back()
	case key.Matches(msg, formKeys.Next):
		m.form = m.form.Next()
		return m, nil
	case key.Matches(msg, formKeys.Prev):
		m.form = m.form.Prev()
		return m, nil
	case key.Matches(msg, formKeys.Edit):
		switch {
		case field == fieldMode:
			return m.submit()
		case field == fieldBranch && m.hasRow:
			r := m.row
			m.branchPicker = m.branchPicker.Open("Branch for "+r.Workspace.Name, r.Workspace.ID, r.Local, r.Remote, r.DefaultBranch, r.Branch)
			m.overlay = overlayBranch
			return m, nil
		default:
			return m.openWorkspacePicker(), nil
		}
	}
	if field == fieldMode {
		m.mode, _ = m.mode.Update(msg)
	}
	return m, nil
}

func (m Model) submit() (Model, tea.Cmd) {
	if m.submitting {
		return m, nil
	}
	if !m.hasRow {
		m.status = ui.Status{Text: "pick a workspace first", Kind: ui.StatusError}
		m.form = m.form.Focus(fieldWorkspace)
		return m, nil
	}
	if m.row.Busy() {
		m.status = ui.Status{Text: "waiting for branch info", Kind: ui.StatusWarning}
		return m, nil
	}
	m.status = ui.Status{}
	m.submitting = true
	return m, provider.AddWorkspaceToSession(m.sessionID, m.row.Spec(branchfield.IsNewBranch(m.mode)))
}

func (m Model) View() string {
	switch m.overlay {
	case overlayWorkspace:
		return ui.FitHeight(m.workspacePicker.View(), m.height)
	case overlayBranch:
		return ui.FitHeight(m.branchPicker.View(), m.height)
	}
	wsValue := ui.Muted().Render("none — press enter to pick")
	branchValue := ui.Muted().Render("—")
	if m.hasRow {
		wsValue = m.row.Workspace.Name
		branchValue = m.row.BranchView()
	}
	fields := []ui.Field{
		{Label: "Workspace", Value: wsValue},
		{Label: "Branch", Value: branchValue},
		{Label: "Mode", Value: m.mode.View(m.form.Focused() == fieldMode)},
	}
	return ui.FitHeight(m.form.View(fields, m.width), m.height)
}
