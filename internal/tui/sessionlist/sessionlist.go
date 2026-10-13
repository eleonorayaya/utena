package sessionlist

import (
	"fmt"
	"sort"
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

type listGroupKind int

const (
	groupLive listGroupKind = iota
	groupBlocked
	groupHidden
)

func listGroup(s session.Session) listGroupKind {
	switch {
	case s.IsHidden():
		return groupHidden
	case s.IsBlocked():
		return groupBlocked
	default:
		return groupLive
	}
}

func (m Model) firstBlockedIndex() int {
	for i, s := range m.filtered {
		if listGroup(s) == groupBlocked {
			return i
		}
	}
	return -1
}

func (m Model) bodyHeight() int {
	h := m.height - 3
	if m.firstBlockedIndex() >= 0 {
		h -= 2
	}
	if h < 1 {
		return 1
	}
	return h
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
	m.statusMsg = prompt
	return nil
}

func (m *Model) clearPending() {
	m.pendingArchiveID = 0
	m.pendingDeleteID = 0
	m.pendingRepairID = 0
	m.statusMsg = ""
}

func (m *Model) clampOffset() {
	bh := m.bodyHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+bh {
		m.offset = m.cursor - bh + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
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
	sort.SliceStable(m.filtered, func(i, j int) bool {
		return listGroup(m.filtered[i]) < listGroup(m.filtered[j])
	})

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
		m.statusMsg = msg.Err.Error()
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
		m.statusMsg = ""
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
			m.statusMsg = "already attached to this session"
			return m, nil, true
		}
		switch sel.Status {
		case session.StatusCreating:
			m.statusMsg = "session is still being created"
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
			m.statusMsg = errMsg + " — press enter again to repair"
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
			m.statusMsg = "cannot archive attached session"
			return m, nil, true
		}
		if sel.Status == session.StatusArchived {
			m.statusMsg = "already archived"
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
			m.statusMsg = "cannot delete attached session"
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
	var badgeStr string
	if listGroup(s) == groupBlocked {
		badgeStr = lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(" " + blockedTag(s) + " ")
	} else {
		badgeStr = statusview.NewStatusBadge(s.ClaudeSessions, false).View()
	}
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

func blockedTag(s session.Session) string {
	open := s.OpenBlockers()
	tag := "⏸ " + string(open[0].Kind)
	if len(open) > 1 {
		tag += fmt.Sprintf(" ×%d", len(open))
	}
	return tag
}

func sectionHeader(title string, width int) string {
	titleStyle := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
	return titleStyle.Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant).Render(strings.Repeat("─", width))
}

func (m Model) renderList(width int) string {
	var b strings.Builder

	b.WriteString(sectionHeader("Sessions", width))
	b.WriteString("\n")

	bh := m.bodyHeight()
	end := min(m.offset+bh, len(m.filtered))
	visible := m.filtered
	if m.offset < len(visible) {
		visible = visible[m.offset:end]
	} else {
		visible = nil
	}

	blockedAt := m.firstBlockedIndex()
	for i, s := range visible {
		idx := i + m.offset
		if idx == blockedAt {
			b.WriteString(sectionHeader("Blocked", width))
			b.WriteString("\n")
		}
		selected := idx == m.cursor
		b.WriteString(m.renderRow(s, selected, width))
		if i < len(visible)-1 {
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m Model) renderFooter() string {
	msg := truncateWidth(m.statusMsg, m.width)
	return lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(msg)
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
	listStr := m.renderList(listW)
	footer := m.renderFooter()

	if !m.isSplit() {
		return listStr + "\n" + footer
	}

	bodyH := m.height - 1
	panelW := m.width - listW - 2
	divStyle := lipgloss.NewStyle().Foreground(theme.Current.TextMuted)
	divLines := make([]string, bodyH)
	for i := range divLines {
		divLines[i] = divStyle.Render("│ ")
	}
	divider := strings.Join(divLines, "\n")

	sel := m.selectedSession()
	panelStr := sessiondetail.RenderPanel(sel, panelW, bodyH)

	body := lipgloss.JoinHorizontal(lipgloss.Top, listStr, divider, panelStr)
	return body + "\n" + footer
}
