package sessionlist

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessiondetail"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const (
	splitThreshold  = 90
	listGutter      = 2
	outdatedWarning = "browser extension outdated"
)

type Model struct {
	sessions         []session.Session
	filtered         []session.Session
	cursor           int
	offset           int
	showHidden       bool
	pendingArchiveID uint
	pendingDeleteID  uint
	pendingRepairID  uint
	statusMsg        string
	statusKind       ui.StatusKind
	browserState     browser.SessionsView
	detail           sessiondetail.Model
	width            int
	height           int
	escQuits         bool
}

type Option func(*Model)

func WithEscQuits() Option {
	return func(m *Model) { m.escQuits = true }
}

func New(opts ...Option) Model {
	m := Model{detail: sessiondetail.New()}
	for _, opt := range opts {
		opt(&m)
	}
	return m
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, tea.Batch(provider.FetchSessions(), provider.RequestBrowserState(), m.detail.Init())
}

func (m Model) Keys() help.KeyMap {
	if m.detail.Focused() {
		return m.detail.Keys()
	}
	return m.keys()
}

func (m Model) keys() keyMap {
	if m.escQuits {
		return pickerKeys
	}
	return keys
}

func (m Model) CapturesText() bool {
	return m.detail.Focused() && m.detail.CapturesText()
}

func (m Model) isSplit() bool {
	return m.width >= splitThreshold
}

func (m Model) listWidth() int {
	w := m.width * 2 / 5
	return min(max(w, 36), 52)
}

func (m Model) bodyHeight() int {
	return max(m.height, 1)
}

func (m Model) panelWidth() int {
	if !m.isSplit() {
		return m.width
	}
	return m.width - m.listWidth() - 2
}

func (m Model) selectedID() uint {
	if sel := m.selectedSession(); sel != nil {
		return sel.ID
	}
	return 0
}

func (m Model) selectedSession() *session.Session {
	if len(m.filtered) == 0 || m.cursor < 0 || m.cursor >= len(m.filtered) {
		return nil
	}
	s := m.filtered[m.cursor]
	return &s
}

func (m *Model) confirmTwoPress(pending *uint, id uint, action tea.Cmd, prompt string) tea.Cmd {
	if *pending == id {
		*pending = 0
		return action
	}
	*pending = id
	m.setStatus(prompt, ui.StatusWarning)
	return nil
}

func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	if m.detail.Focused() {
		if st := m.detail.Status(); !st.Empty() {
			return st
		}
	}
	if m.statusMsg != "" {
		return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
	}
	return m.browserWarning()
}

func (m Model) browserWarning() ui.Status {
	if m.browserState.Outdated {
		text := outdatedWarning
		if m.browserState.UpdateHint != "" {
			text += " — " + m.browserState.UpdateHint
		}
		return ui.Status{Text: text, Kind: ui.StatusWarning}
	}
	if m.browserState.Warning != "" {
		return ui.Status{Text: m.browserState.Warning, Kind: ui.StatusWarning}
	}
	return ui.Status{}
}

func (m *Model) clearPending() {
	m.pendingArchiveID = 0
	m.pendingDeleteID = 0
	m.pendingRepairID = 0
	m.setStatus("", ui.StatusInfo)
}

func (m *Model) clampOffset() {
	rows := layoutRows(m.filtered)
	bh := m.bodyHeight()
	cr := rowOfSession(rows, m.cursor)
	if cr < 0 {
		m.offset = 0
		return
	}
	top := cr
	if cr > 0 && rows[cr-1].kind == rowHeader && bh > 1 {
		top = cr - 1
	}
	m.offset = ui.ScrollOffset(m.offset, top, cr, len(rows), bh)
}

func (m *Model) rebuildFiltered() {
	var selectedID uint
	if sel := m.selectedSession(); sel != nil {
		selectedID = sel.ID
	}

	m.filtered = nil
	for _, s := range m.sessions {
		if s.ListVisible(m.showHidden) {
			m.filtered = append(m.filtered, s)
		}
	}
	sortSessions(m.filtered)

	if selectedID != 0 {
		for i, s := range m.filtered {
			if s.ID == selectedID {
				m.cursor = i
				m.clampOffset()
				return
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampOffset()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.OnWindowSizeMsg(msg)
	case tea.KeyMsg:
		if m.detail.Focused() {
			m.setStatus("", ui.StatusInfo)
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		}
		m, cmd, _ := m.OnKeyMsg(msg)
		return m, cmd
	}

	var detailCmd tea.Cmd
	m.detail, detailCmd = m.detail.Update(msg)

	switch msg := msg.(type) {
	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		prevID := m.selectedID()
		m.rebuildFiltered()
		if id := m.selectedID(); id == 0 || id != prevID {
			m.clearPending()
		}
		m.detail = m.detail.Select(m.selectedSession())
	case provider.BrowserStateUpdatedMsg:
		m.browserState = msg.State
	case ui.OpenURLFailedMsg:
		m.setStatus("could not open PR: "+msg.Err.Error(), ui.StatusError)
	case provider.ErrMsg:
		m.setStatus(msg.Err.Error(), ui.StatusError)
	case provider.SessionSwitchedMsg:
		return m, tea.Quit
	}
	return m, detailCmd
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.clampOffset()
	var cmd tea.Cmd
	m.detail, cmd = m.detail.Update(tea.WindowSizeMsg{Width: m.panelWidth(), Height: m.bodyHeight()})
	return m, cmd
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	prevID := m.selectedID()
	m, cmd, handled := m.onKeyMsg(msg)
	if id := m.selectedID(); id != 0 && id != prevID {
		m.clearPending()
	}
	m.detail = m.detail.Select(m.selectedSession())
	return m, cmd, handled
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {

	isNav := key.Matches(msg, keys.Up) || key.Matches(msg, keys.Down)
	if !key.Matches(msg, keys.Archive) && !isNav {
		m.pendingArchiveID = 0
	}
	if !key.Matches(msg, keys.Delete) && !isNav {
		m.pendingDeleteID = 0
	}
	if !key.Matches(msg, keys.Select) && !isNav {
		m.pendingRepairID = 0
	}
	if !isNav {
		m.setStatus("", ui.StatusInfo)
	}

	switch {
	case key.Matches(msg, m.keys().Quit):
		return m, tea.Quit, true

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
		return m, nil, true

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.clampOffset()
		}
		return m, nil, true

	case key.Matches(msg, keys.Focus):
		if m.selectedSession() == nil {
			return m, nil, false
		}
		m.clearPending()
		m.detail = m.detail.Focus()
		return m, nil, true

	case key.Matches(msg, keys.ToggleHidden):
		m.showHidden = !m.showHidden
		m.rebuildFiltered()
		return m, nil, true

	case key.Matches(msg, keys.New):
		return m, router.NavigateTo(router.SessionFormView), true

	case key.Matches(msg, keys.Workspaces):
		return m, router.NavigateTo(router.WorkspaceListView), true

	case key.Matches(msg, keys.Select):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		if sel.IsAttached {
			m.setStatus("already attached to this session", ui.StatusInfo)
			return m, nil, true
		}
		switch sel.Status {
		case session.StatusCreating:
			m.setStatus("session is still being created", ui.StatusInfo)
			return m, nil, true
		case session.StatusBroken:
			if m.pendingRepairID == sel.ID {
				m.pendingRepairID = 0
				id := sel.ID
				return m, tea.Batch(
					provider.RepairSession(id),
					tea.Sequence(
						router.NavigateTo(router.SessionProgressView),
						sessionprogress.StartRepair(id),
					),
				), true
			}
			errMsg := "broken"
			if sel.StatusError != "" {
				errMsg = sel.StatusError
			}
			m.pendingRepairID = sel.ID
			m.setStatus(errMsg+" — press enter again to repair", ui.StatusWarning)
			return m, nil, true
		default:
			return m, provider.ActivateSession(sel.ID), true
		}

	case key.Matches(msg, keys.OpenPR):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		prs := ui.OpenablePRs(sel.PullRequests)
		switch len(prs) {
		case 0:
			m.setStatus("no open pull requests", ui.StatusInfo)
			return m, nil, true
		case 1:
			return m, provider.OpenURL(sel.ID, prs[0].HTMLURL), true
		default:
			m.clearPending()
			m.detail = m.detail.FocusPR()
			return m, nil, true
		}

	case key.Matches(msg, keys.Archive):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		if sel.IsAttached {
			m.setStatus("cannot archive attached session", ui.StatusInfo)
			return m, nil, true
		}
		if sel.Status == session.StatusArchived {
			m.setStatus("already archived", ui.StatusInfo)
			return m, nil, true
		}
		cmd := m.confirmTwoPress(&m.pendingArchiveID, sel.ID, provider.ArchiveSession(sel.ID),
			fmt.Sprintf("press a again to archive %s", sessionDisplayName(*sel)))
		return m, cmd, true

	case key.Matches(msg, keys.Block):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		return m, tea.Sequence(
			router.NavigateTo(router.BlockerFormView),
			blockerform.Start(sel.ID),
		), true

	case key.Matches(msg, keys.Delete):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		if sel.IsAttached {
			m.setStatus("cannot delete attached session", ui.StatusInfo)
			return m, nil, true
		}
		// Pending sessions hold no real resources; dismiss them outright.
		if sel.Status == session.StatusPending {
			return m, provider.DismissSession(sel.ID), true
		}
		cmd := m.confirmTwoPress(&m.pendingDeleteID, sel.ID, provider.DeleteSession(sel.ID, true),
			fmt.Sprintf("press d again to delete %s (removes worktrees)", sessionDisplayName(*sel)))
		return m, cmd, true
	}

	return m, nil, false
}

func sessionDisplayName(s session.Session) string {
	if s.Name != "" {
		return s.Name
	}
	if s.MuxSession != nil && s.MuxSession.Name != "" {
		return s.MuxSession.Name
	}
	return fmt.Sprintf("%d", s.ID)
}

func (m Model) renderList(width int) string {
	if len(m.filtered) == 0 {
		empty := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render("  No sessions — press n to create one")
		return lipgloss.NewStyle().Width(width).Render(empty)
	}
	rows := layoutRows(m.filtered)
	end := min(m.offset+m.bodyHeight(), len(rows))
	var lines []string
	for _, r := range rows[m.offset:end] {
		switch r.kind {
		case rowGap:
			lines = append(lines, "")
		case rowHeader:
			lines = append(lines, "  "+ui.SectionHeader(r.section.title(), r.count))
		default:
			lines = append(lines, m.renderRow(m.filtered[r.index], r.index == m.cursor, width))
		}
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) dividerColor() lipgloss.Color {
	if m.detail.Focused() {
		return theme.Current.Primary
	}
	return theme.Current.SurfaceVariant
}

func (m Model) View() string {
	h := m.bodyHeight()
	if !m.isSplit() {
		if m.detail.Focused() {
			return ui.FitHeight(m.detail.View(), h)
		}
		return ui.FitHeight(m.renderList(m.width), h)
	}

	listStr := ui.FitHeight(m.renderList(m.listWidth()-listGutter), h)
	divStyle := lipgloss.NewStyle().Foreground(m.dividerColor())
	divLines := make([]string, h)
	for i := range divLines {
		divLines[i] = strings.Repeat(" ", listGutter) + divStyle.Render("│ ")
	}
	divider := strings.Join(divLines, "\n")

	panelStr := ui.FitHeight(m.detail.View(), h)
	return lipgloss.JoinHorizontal(lipgloss.Top, listStr, divider, panelStr)
}
