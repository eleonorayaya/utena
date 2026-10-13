package sessionlist

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessiondetail"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/statusview"
	"github.com/eleonorayaya/utena/internal/tui/theme"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

const splitThreshold = 90

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
	width            int
	height           int
}

func New() Model {
	return Model{}
}

func (m Model) Init() (Model, tea.Cmd) {
	return m, provider.FetchSessions()
}

func (m Model) Keys() help.KeyMap {
	return keys
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
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
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
	if top < m.offset {
		m.offset = top
	}
	if cr >= m.offset+bh {
		m.offset = cr - bh + 1
	}
	m.offset = max(min(m.offset, len(rows)-bh), 0)
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
	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		prevSel := m.selectedSession()
		m.rebuildFiltered()
		newSel := m.selectedSession()
		if prevSel == nil || newSel == nil || prevSel.ID != newSel.ID {
			m.clearPending()
		}
		return m, nil
	case provider.ErrMsg:
		m.setStatus(msg.Err.Error(), ui.StatusError)
		return m, nil
	case provider.SessionSwitchedMsg:
		return m, tea.Quit
	case tea.KeyMsg:
		var cmd tea.Cmd
		var handled bool
		m, cmd, handled = m.OnKeyMsg(msg)
		if handled {
			return m, cmd
		}
	}
	return m, nil
}

func (m Model) OnWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
	m.width = msg.Width
	m.height = msg.Height
	m.clampOffset()
	return m, nil
}

func (m Model) OnKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	prevID := uint(0)
	if sel := m.selectedSession(); sel != nil {
		prevID = sel.ID
	}

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
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit, true

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clampOffset()
		}
		if sel := m.selectedSession(); sel != nil && sel.ID != prevID {
			m.clearPending()
		}
		return m, nil, true

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.clampOffset()
		}
		if sel := m.selectedSession(); sel != nil && sel.ID != prevID {
			m.clearPending()
		}
		return m, nil, true

	case key.Matches(msg, keys.Info):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		return m, tea.Sequence(
			router.NavigateTo(router.SessionDetailView),
			sessiondetail.Select(*sel),
		), true

	case key.Matches(msg, keys.ToggleHidden):
		m.showHidden = !m.showHidden
		m.rebuildFiltered()
		return m, nil, true

	case key.Matches(msg, keys.New):
		return m, router.NavigateTo(router.SessionFormView), true

	case key.Matches(msg, keys.Todos):
		return m, router.NavigateTo(router.TodoListView), true

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
						sessionprogress.Start(id),
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
	if s.TmuxSession != nil && s.TmuxSession.Name != "" {
		return s.TmuxSession.Name
	}
	return fmt.Sprintf("%d", s.ID)
}

func (m Model) renderRow(s session.Session, selected bool, width int) string {
	badgeStr := statusview.NewStatusBadge(s.ClaudeSessions, false).View()
	badgeW := lipgloss.Width(badgeStr)

	var timePart string
	if !s.LastUsedAt.IsZero() {
		timePart = " " + common.TimeAgo(s.LastUsedAt) + " "
	}
	timePartW := lipgloss.Width(timePart)

	const wsColW = 19
	showWS := !m.isSplit()
	wsPartW := 0
	if showWS {
		wsPartW = wsColW
	}

	const cursorW = 2
	nameW := max(width-cursorW-wsPartW-timePartW-badgeW, 1)

	name := truncateWidth(sessionDisplayName(s), nameW)
	namePad := strings.Repeat(" ", max(nameW-lipgloss.Width(name), 0))

	var cursorStr string
	if selected {
		cursorStr = lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render("› ")
	} else if s.IsAttached {
		cursorStr = lipgloss.NewStyle().Foreground(theme.Current.StatusReady).Render("· ")
	} else {
		cursorStr = "  "
	}

	var nameStyle lipgloss.Style
	switch s.Status {
	case session.StatusBroken:
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.Error)
	case session.StatusCreating:
		nameStyle = lipgloss.NewStyle().Foreground(theme.Current.StatusActive)
	default:
		if selected {
			nameStyle = lipgloss.NewStyle().Foreground(theme.Current.TextEmphasis).Bold(true)
		} else {
			nameStyle = lipgloss.NewStyle().Foreground(theme.Current.Text)
		}
	}
	nameStr := nameStyle.Render(name + namePad)

	var wsStr string
	if showWS {
		const wsTextW = 18
		ws := truncateWidth(s.WorkspaceDisplay(), wsTextW)
		wsPad := strings.Repeat(" ", max(wsTextW-lipgloss.Width(ws), 0))
		wsStr = lipgloss.NewStyle().Foreground(theme.Current.Tertiary).Render(" " + ws + wsPad)
	}

	timeStr := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(timePart)

	return cursorStr + nameStr + wsStr + timeStr + badgeStr
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

func truncateWidth(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for lipgloss.Width(string(runes)) > width-1 && len(runes) > 0 {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}

func (m Model) View() string {
	listW := m.width
	if m.isSplit() {
		listW = m.listWidth()
	}
	listStr := ui.FitHeight(m.renderList(listW), m.height)
	if !m.isSplit() {
		return listStr
	}

	panelW := m.width - listW - 2
	divStyle := lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
	divLines := make([]string, m.height)
	for i := range divLines {
		divLines[i] = divStyle.Render("│ ")
	}
	divider := strings.Join(divLines, "\n")

	panelStr := ui.FitHeight(sessiondetail.RenderPanel(m.selectedSession(), panelW, m.height), m.height)
	return lipgloss.JoinHorizontal(lipgloss.Top, listStr, divider, panelStr)
}
