package addworkspaceform

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/branchfield"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/theme"
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
	row             *branchfield.Row
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
	m.row = nil
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

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.SetSize(msg.Width, msg.Height)
	return m, nil
}

func (m Model) OnKeyMsg(_ tea.KeyMsg) (Model, tea.Cmd, bool) {
	return m, nil, false
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
	return m.row != nil && m.row.Editing
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case StartMsg:
		m = m.reset()
		m.sessionID = msg.SessionID
		return m, provider.RequestWorkspacesState()
	case provider.WorkspacesStateUpdatedMsg:
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	case provider.BranchesLoadedMsg:
		if m.row != nil && m.row.Workspace.ID == msg.WorkspaceID {
			r := m.row.Loaded(msg)
			m.row = &r
		}
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchesFetchedMsg:
		if m.row != nil && m.row.Workspace.ID == msg.WorkspaceID {
			r := m.row.Fetched(msg)
			m.row = &r
		}
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchExistsCheckedMsg:
		if m.row != nil {
			r := m.row.Checked(msg)
			m.row = &r
		}
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
		if m.row != nil && m.row.Workspace.ID == ws.ID {
			return m, nil
		}
		r := branchfield.NewRow(ws)
		m.row = &r
		return m, provider.RequestBranches(ws.ID)
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc && !m.workspacePicker.Filtering() {
			m.overlay = overlayNone
			m.status = ui.Status{}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		r := m.row.Pick(msg.Branch)
		m.row = &r
		m.overlay = overlayNone
		m.form = m.form.Focus(fieldBranch)
		return m, nil
	case branchpicker.FetchRequestedMsg:
		return m, provider.FetchOriginBranches(m.branchPicker.WorkspaceID())
	case branchpicker.ManualEntryRequestedMsg:
		m.overlay = overlayNone
		m.form = m.form.Focus(fieldBranch)
		r, cmd := m.row.StartManual()
		m.row = &r
		return m, cmd
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc && !m.branchPicker.Filtering() {
			m.overlay = overlayNone
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m Model) openWorkspacePicker() Model {
	var ids []uint
	if m.row != nil {
		ids = []uint{m.row.Workspace.ID}
	}
	m.workspacePicker = m.workspacePicker.Open(ids)
	m.overlay = overlayWorkspace
	return m
}

func (m Model) onPageKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.row != nil && m.row.Editing {
		r, cmd := m.row.UpdateManual(msg)
		m.row = &r
		return m, cmd
	}
	field := m.form.Focused()
	switch msg.String() {
	case "esc":
		return m, router.Back()
	case "tab", "down":
		m.form = m.form.Next()
		return m, nil
	case "shift+tab", "up":
		m.form = m.form.Prev()
		return m, nil
	case "enter":
		switch field {
		case fieldWorkspace:
			return m.openWorkspacePicker(), nil
		case fieldBranch:
			if m.row == nil {
				return m.openWorkspacePicker(), nil
			}
			r := m.row
			m.branchPicker = m.branchPicker.Open("Branch for "+r.Workspace.Name, r.Workspace.ID, r.Local, r.Remote, r.DefaultBranch, r.Branch)
			m.overlay = overlayBranch
			return m, nil
		case fieldMode:
			return m.submit()
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
	if m.row == nil {
		m.status = ui.Status{Text: "pick a workspace first", Kind: ui.StatusError}
		m.form = m.form.Focus(fieldWorkspace)
		return m, nil
	}
	if m.row.Busy() {
		m.status = ui.Status{Text: "waiting for branch info", Kind: ui.StatusWarning}
		return m, nil
	}
	spec := provider.SessionWorkspaceSpec{WorkspaceID: m.row.Workspace.ID}
	if branchfield.IsNewBranch(m.mode) {
		spec.BaseBranch = m.row.Branch
	} else {
		spec.Branch = m.row.Branch
	}
	m.status = ui.Status{}
	m.submitting = true
	return m, provider.AddWorkspaceToSession(m.sessionID, spec)
}

func (m Model) View() string {
	switch m.overlay {
	case overlayWorkspace:
		return ui.FitHeight(m.workspacePicker.View(), m.height)
	case overlayBranch:
		return ui.FitHeight(m.branchPicker.View(), m.height)
	}
	muted := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
	focused := m.form.Focused()
	wsValue := muted.Render("none — press enter to pick")
	branchValue := muted.Render("—")
	if m.row != nil {
		wsValue = m.row.Workspace.Name
		branchValue = m.row.BranchView()
	}
	fields := []ui.Field{
		{Label: "Workspace", Value: wsValue},
		{Label: "Branch", Value: branchValue},
		{Label: "Mode", Value: m.mode.View(focused == fieldMode)},
	}
	return ui.FitHeight(m.form.View(fields, m.width), m.height)
}
