package sessionform

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/branchfield"
	"github.com/eleonorayaya/utena/internal/tui/branchpicker"
	"github.com/eleonorayaya/utena/internal/tui/filepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
	"github.com/eleonorayaya/utena/internal/tui/workspacepicker"
	"github.com/eleonorayaya/utena/internal/workspace"
)

const (
	fieldWorkspaces = iota
	fieldBranches
	fieldMode
	fieldName
	fieldCount
)

type overlay int

const (
	overlayNone overlay = iota
	overlayWorkspaces
	overlayFiles
	overlayBranch
)

type Model struct {
	form            ui.Form
	overlay         overlay
	workspacePicker workspacepicker.Model
	filePicker      filepicker.Model
	branchPicker    branchpicker.Model
	rows            []branchfield.Row
	rowCursor       int
	pickingRow      int
	mode            ui.Choice
	name            textinput.Model
	nameErr         string
	workspacesErr   string
	status          ui.Status
	submitting      bool
	width, height   int
}

var labels = [fieldCount]string{"Workspaces", "Branches", "Mode", "Name"}

func New() Model {
	return Model{
		form:            ui.NewForm("New session", fieldCount),
		workspacePicker: workspacepicker.New("Workspaces", true),
		branchPicker:    branchpicker.New(),
		filePicker:      filepicker.New(),
		mode:            branchfield.NewModeChoice(),
		name:            ui.NewInput(""),
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	m.form = m.form.Focus(fieldName)
	m.rows = nil
	m.rowCursor = 0
	m.mode = branchfield.NewModeChoice()
	m.name = ui.NewInput("")
	m.name.Width = ui.InputWidth(m.width, labels[:]...)
	m.nameErr = ""
	m.workspacesErr = ""
	m.status = ui.Status{}
	m.submitting = false
	m.overlay = overlayWorkspaces
	m.workspacePicker = m.workspacePicker.Open(nil)
	return m, tea.Batch(provider.FetchWorkspaces(), provider.RequestWorkspacesState())
}

func (m *Model) SetSize(width, height int) {
	m.width, m.height = width, height
	m.name.Width = ui.InputWidth(width, labels[:]...)
	m.workspacePicker.SetSize(width, height)
	m.filePicker.SetSize(width, height)
	m.branchPicker.SetSize(width, height)
}

func (m Model) Keys() help.KeyMap {
	switch m.overlay {
	case overlayWorkspaces:
		return m.workspacePicker.Keys()
	case overlayFiles:
		return m.filePicker.Keys()
	case overlayBranch:
		return m.branchPicker.Keys()
	}
	k := formKeys
	onName := m.form.Focused() == fieldName
	k.Create.SetEnabled(onName)
	k.Edit.SetEnabled(!onName)
	return k
}

func (m Model) Status() ui.Status {
	switch m.overlay {
	case overlayFiles:
		if s := m.filePicker.Status(); !s.Empty() {
			return s
		}
	case overlayBranch:
		if s := m.branchPicker.Status(); !s.Empty() {
			return s
		}
	}
	return m.status
}

func (m Model) CapturesText() bool {
	switch m.overlay {
	case overlayWorkspaces:
		return m.workspacePicker.CapturesText()
	case overlayFiles:
		return m.filePicker.CapturesText()
	case overlayBranch:
		return m.branchPicker.CapturesText()
	}
	return m.form.Focused() == fieldName || m.editingRow() >= 0
}

func (m Model) editingRow() int {
	return slices.IndexFunc(m.rows, func(r branchfield.Row) bool { return r.Editing })
}

func (m Model) rowIndex(wsID uint) int {
	return slices.IndexFunc(m.rows, func(r branchfield.Row) bool { return r.Workspace.ID == wsID })
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
		return m, nil
	case provider.WorkspacesStateUpdatedMsg:
		var cmd tea.Cmd
		m.workspacePicker, cmd = m.workspacePicker.Update(msg)
		return m, cmd
	case provider.BranchesLoadedMsg:
		if i := m.rowIndex(msg.WorkspaceID); i >= 0 {
			m.rows[i] = m.rows[i].Loaded(msg)
		}
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchesFetchedMsg:
		if i := m.rowIndex(msg.WorkspaceID); i >= 0 {
			m.rows[i] = m.rows[i].Fetched(msg)
		}
		var cmd tea.Cmd
		m.branchPicker, cmd = m.branchPicker.Update(msg)
		return m, cmd
	case provider.BranchExistsCheckedMsg:
		if i := m.rowIndex(msg.WorkspaceID); i >= 0 {
			m.rows[i] = m.rows[i].Checked(msg)
		}
		return m, nil
	case provider.ErrMsg:
		m.submitting = false
		m.status = ui.Status{Text: msg.Err.Error(), Kind: ui.StatusError}
		return m, nil
	case provider.SessionCreatedMsg:
		m.submitting = false
		return m, tea.Sequence(router.NavigateTo(router.SessionProgressView), sessionprogress.Start(msg.ID))
	}

	switch m.overlay {
	case overlayWorkspaces:
		return m.updateWorkspacePicker(msg)
	case overlayFiles:
		return m.updateFilePicker(msg)
	case overlayBranch:
		return m.updateBranchPicker(msg)
	}

	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		return m.onPageKey(keyMsg)
	}
	if m.form.Focused() == fieldName {
		var cmd tea.Cmd
		m.name, cmd = m.name.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) openWorkspacePicker() Model {
	ids := make([]uint, len(m.rows))
	for i, r := range m.rows {
		ids[i] = r.Workspace.ID
	}
	m.workspacePicker = m.workspacePicker.Open(ids)
	m.overlay = overlayWorkspaces
	m.status = ui.Status{}
	return m
}

func (m Model) openBranchPicker(i int) Model {
	r := m.rows[i]
	m.pickingRow = i
	m.branchPicker = m.branchPicker.Open("Branch for "+r.Workspace.Name, r.Workspace.ID, r.Local, r.Remote, r.DefaultBranch, r.Branch)
	m.overlay = overlayBranch
	return m
}

func (m Model) setRows(picked []workspace.Workspace) (Model, tea.Cmd) {
	var rows []branchfield.Row
	var cmds []tea.Cmd
	for _, ws := range picked {
		if i := m.rowIndex(ws.ID); i >= 0 {
			rows = append(rows, m.rows[i])
			continue
		}
		rows = append(rows, branchfield.NewRow(ws))
		cmds = append(cmds, provider.RequestBranches(ws.ID))
	}
	m.rows = rows
	m.rowCursor = min(m.rowCursor, max(len(rows)-1, 0))
	return m, tea.Batch(cmds...)
}

func (m Model) focus(field int) (Model, tea.Cmd) {
	m.form = m.form.Focus(field)
	if field == fieldName {
		return m, m.name.Focus()
	}
	m.name.Blur()
	return m, nil
}

func (m Model) updateWorkspacePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case workspacepicker.SelectedMsg:
		for _, ws := range msg.Workspaces {
			if !ws.IsGitRepo {
				m.status = ui.Status{Text: ws.Name + " is not a git repository", Kind: ui.StatusError}
				return m, nil
			}
		}
		m.overlay = overlayNone
		m.workspacesErr = ""
		m.status = ui.Status{}
		var rowsCmd, focusCmd tea.Cmd
		m, rowsCmd = m.setRows(msg.Workspaces)
		m.name.Placeholder = defaultSessionName(msg.Workspaces[0].Name)
		m, focusCmd = m.focus(fieldName)
		return m, tea.Batch(rowsCmd, focusCmd)
	case workspacepicker.AddDirectoryMsg:
		m.filePicker = filepicker.New()
		m.filePicker.SetSize(m.width, m.height)
		m.filePicker, _ = m.filePicker.Init()
		m.overlay = overlayFiles
		return m, nil
	case workspacepicker.ClosedMsg:
		m.overlay = overlayNone
		m.status = ui.Status{}
		if len(m.rows) == 0 {
			return m.focus(fieldWorkspaces)
		}
		return m.focus(m.form.Focused())
	}
	var cmd tea.Cmd
	m.workspacePicker, cmd = m.workspacePicker.Update(msg)
	return m, cmd
}

func (m Model) updateFilePicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case filepicker.DirectorySelectedMsg:
		m.overlay = overlayWorkspaces
		return m, provider.AddWorkspace(msg.Path, true)
	case filepicker.ClosedMsg:
		m.overlay = overlayWorkspaces
		return m, nil
	}
	var cmd tea.Cmd
	m.filePicker, cmd = m.filePicker.Update(msg)
	return m, cmd
}

func (m Model) updateBranchPicker(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case branchpicker.SelectedMsg:
		m.rows[m.pickingRow] = m.rows[m.pickingRow].Pick(msg.Branch)
		m.overlay = overlayNone
		m.rowCursor = m.pickingRow
		return m.focus(fieldBranches)
	case branchpicker.FetchRequestedMsg:
		return m, provider.FetchOriginBranches(m.branchPicker.WorkspaceID())
	case branchpicker.ManualEntryRequestedMsg:
		m.overlay = overlayNone
		m.rowCursor = m.pickingRow
		m, _ = m.focus(fieldBranches)
		var cmd tea.Cmd
		m.rows[m.pickingRow], cmd = m.rows[m.pickingRow].StartManual()
		return m, cmd
	case branchpicker.ClosedMsg:
		m.overlay = overlayNone
		return m, nil
	}
	var cmd tea.Cmd
	m.branchPicker, cmd = m.branchPicker.Update(msg)
	return m, cmd
}

func (m Model) onPageKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if i := m.editingRow(); i >= 0 {
		var cmd tea.Cmd
		m.rows[i], cmd = m.rows[i].UpdateManual(msg)
		return m, cmd
	}

	field := m.form.Focused()
	switch {
	case key.Matches(msg, formKeys.Cancel):
		return m, router.Back()
	case msg.Type == tea.KeyDown && field == fieldBranches && m.rowCursor < len(m.rows)-1:
		m.rowCursor++
		return m, nil
	case msg.Type == tea.KeyUp && field == fieldBranches && m.rowCursor > 0:
		m.rowCursor--
		return m, nil
	case key.Matches(msg, formKeys.Next):
		if field == fieldWorkspaces && msg.Type == tea.KeyDown {
			m.rowCursor = 0
		}
		return m.focus(m.form.Next().Focused())
	case key.Matches(msg, formKeys.Prev):
		if field == fieldMode && msg.Type == tea.KeyUp {
			m.rowCursor = max(len(m.rows)-1, 0)
		}
		return m.focus(m.form.Prev().Focused())
	case key.Matches(msg, formKeys.Edit):
		switch field {
		case fieldWorkspaces:
			return m.openWorkspacePicker(), nil
		case fieldBranches:
			if len(m.rows) == 0 {
				return m.openWorkspacePicker(), nil
			}
			return m.openBranchPicker(m.rowCursor), nil
		case fieldMode:
			m.mode, _ = m.mode.Update(tea.KeyMsg{Type: tea.KeySpace})
			return m, nil
		case fieldName:
			return m.submit()
		}
	}

	switch field {
	case fieldMode:
		m.mode, _ = m.mode.Update(msg)
		return m, nil
	case fieldName:
		var cmd tea.Cmd
		m.name, cmd = m.name.Update(msg)
		m.nameErr = ""
		return m, cmd
	}
	return m, nil
}

func (m Model) submit() (Model, tea.Cmd) {
	if m.submitting {
		return m, nil
	}
	if len(m.rows) == 0 {
		m.workspacesErr = "pick at least one workspace"
		return m.focus(fieldWorkspaces)
	}
	for _, r := range m.rows {
		if r.Busy() {
			m.status = ui.Status{Text: "waiting for branch info", Kind: ui.StatusWarning}
			return m, nil
		}
	}
	isNew := branchfield.IsNewBranch(m.mode)
	name := strings.TrimSpace(m.name.Value())
	if name == "" && isNew {
		name = m.name.Placeholder
	}
	if name != "" {
		if err := session.ValidateSessionName(name); err != nil {
			m.nameErr = err.Error()
			return m, nil
		}
	}
	m.nameErr = ""
	m.status = ui.Status{}
	m.submitting = true
	return m, provider.CreateSession(name, buildSpecs(m.rows, isNew))
}

func buildSpecs(rows []branchfield.Row, isNew bool) []provider.SessionWorkspaceSpec {
	out := make([]provider.SessionWorkspaceSpec, len(rows))
	for i, r := range rows {
		out[i] = r.Spec(isNew)
	}
	return out
}

func defaultSessionName(workspaceName string) string {
	return strings.ToLower(strings.ReplaceAll(workspaceName, " ", "-"))
}

func (m Model) fields() []ui.Field {
	muted := ui.Muted()
	focused := m.form.Focused()

	names := make([]string, len(m.rows))
	for i, r := range m.rows {
		names[i] = r.Workspace.Name
	}
	wsValue := strings.Join(names, ", ")
	if wsValue == "" {
		wsValue = muted.Render("none — press enter to pick")
	}

	branches := branchfield.RowsView(m.rows, m.rowCursor, focused == fieldBranches, max(m.width/3, 8))
	if branches == "" {
		branches = muted.Render("—")
	}

	nameHint := ""
	if !branchfield.IsNewBranch(m.mode) {
		nameHint = "optional — derived from the branch when empty"
	}

	return []ui.Field{
		{Label: labels[fieldWorkspaces], Value: wsValue, Error: m.workspacesErr},
		{Label: labels[fieldBranches], Value: branches},
		{Label: labels[fieldMode], Value: m.mode.View(focused == fieldMode)},
		{Label: labels[fieldName], Value: m.name.View(), Error: m.nameErr, Hint: nameHint},
	}
}

func (m Model) View() string {
	switch m.overlay {
	case overlayWorkspaces:
		return ui.FitHeight(m.workspacePicker.View(), m.height)
	case overlayFiles:
		return ui.FitHeight(m.filePicker.View(), m.height)
	case overlayBranch:
		return ui.FitHeight(m.branchPicker.View(), m.height)
	}
	return ui.FitHeight(m.form.View(m.fields(), m.width), m.height)
}
