package sessionlist

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessiondetail"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/statusview"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

const splitThreshold = 90

type Model struct {
	sessions        []session.Session
	filtered        []session.Session
	cursor          int
	offset          int
	showBroken      bool
	pendingDeleteID uint
	pendingRepairID uint
	statusMsg       string
	prs             []git.PullRequest
	lastPRSessionID uint
	width           int
	height          int
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
	h := m.height - 3
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
		if s.Status == session.StatusDeleted {
			continue
		}
		if s.Status == session.StatusBroken && !m.showBroken {
			continue
		}
		m.filtered = append(m.filtered, s)
	}

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

func (m *Model) maybeFetchPRs() tea.Cmd {
	sel := m.selectedSession()
	if sel == nil {
		m.prs = nil
		return nil
	}
	if sel.ID == m.lastPRSessionID {
		return nil
	}
	m.prs = nil
	if !sel.IsMulti() && len(sel.Worktrees) > 0 && sel.Worktrees[0].Workspace != nil {
		m.lastPRSessionID = sel.ID
		return provider.FetchPRs(sel.Worktrees[0].Workspace.ID, "")
	}
	return nil
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
			m.pendingDeleteID = 0
			m.pendingRepairID = 0
			m.statusMsg = ""
		}
		return m, m.maybeFetchPRs()
	case provider.PRsStateUpdatedMsg:
		sel := m.selectedSession()
		if sel != nil && len(sel.Worktrees) > 0 && sel.Worktrees[0].Workspace != nil &&
			msg.WorkspaceID == sel.Worktrees[0].Workspace.ID {
			var branch *git.Branch
			if sel.Worktrees[0].Worktree != nil {
				branch = sel.Worktrees[0].Worktree.Branch
			}
			m.prs = filterPRsByBranch(msg.PullRequests, branch)
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
	if !key.Matches(msg, keys.Close) && !isNav {
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
			m.pendingDeleteID = 0
			m.pendingRepairID = 0
			m.statusMsg = ""
			return m, m.maybeFetchPRs(), true
		}
		return m, nil, true

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
			m.clampOffset()
		}
		if sel := m.selectedSession(); sel != nil && sel.ID != prevID {
			m.pendingDeleteID = 0
			m.pendingRepairID = 0
			m.statusMsg = ""
			return m, m.maybeFetchPRs(), true
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

	case key.Matches(msg, keys.ToggleBroken):
		m.showBroken = !m.showBroken
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

	case key.Matches(msg, keys.Close):
		sel := m.selectedSession()
		if sel == nil {
			return m, nil, false
		}
		if sel.IsAttached {
			m.statusMsg = "cannot close attached session"
			return m, nil, true
		}
		if m.pendingDeleteID == sel.ID {
			m.pendingDeleteID = 0
			return m, provider.DeleteSession(sel.ID, sel.IsCreating()), true
		}
		m.pendingDeleteID = sel.ID
		if sel.IsCreating() {
			m.statusMsg = forceDeleteMessage(sessionDisplayName(*sel))
			return m, nil, true
		}
		m.statusMsg = fmt.Sprintf("press d again to close %s", sessionDisplayName(*sel))
		return m, nil, true
	}

	return m, nil, false
}

func filterPRsByBranch(prs []git.PullRequest, branch *git.Branch) []git.PullRequest {
	if branch == nil {
		return nil
	}
	var out []git.PullRequest
	for _, pr := range prs {
		if pr.HeadBranchID != nil && *pr.HeadBranchID == branch.ID {
			out = append(out, pr)
		}
	}
	return out
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

func forceDeleteMessage(name string) string {
	return fmt.Sprintf("session is still creating — press d again to force delete %s", name)
}

func (m Model) renderRow(s session.Session, selected bool, width int) string {
	badge := statusview.NewStatusBadge(s.ClaudeSessions, false)
	badgeStr := badge.View()
	badgeW := lipgloss.Width(badgeStr)

	var timePart string
	if !s.LastUsedAt.IsZero() {
		timePart = " " + common.TimeAgo(s.LastUsedAt)
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

	name := sessionDisplayName(s)
	if lipgloss.Width(name) > nameW {
		runes := []rune(name)
		for lipgloss.Width(string(runes)) > nameW-1 && len(runes) > 0 {
			runes = runes[:len(runes)-1]
		}
		name = string(runes) + "…"
	}
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
		ws := s.WorkspaceDisplay()
		const wsTextW = 18
		if lipgloss.Width(ws) > wsTextW {
			runes := []rune(ws)
			for lipgloss.Width(string(runes)) > wsTextW-1 && len(runes) > 0 {
				runes = runes[:len(runes)-1]
			}
			ws = string(runes) + "…"
		}
		wsPad := strings.Repeat(" ", max(wsTextW-lipgloss.Width(ws), 0))
		wsStr = lipgloss.NewStyle().Foreground(theme.Current.Tertiary).Render(" " + ws + wsPad)
	}

	timeStr := lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(timePart)

	return cursorStr + nameStr + wsStr + timeStr + badgeStr
}

func (m Model) renderList(width int) string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true)
	b.WriteString(titleStyle.Render("Sessions"))
	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant).Render(strings.Repeat("─", width)))
	b.WriteString("\n")

	bh := m.bodyHeight()
	end := min(m.offset+bh, len(m.filtered))
	visible := m.filtered
	if m.offset < len(visible) {
		visible = visible[m.offset:end]
	} else {
		visible = nil
	}

	for i, s := range visible {
		idx := i + m.offset
		selected := idx == m.cursor
		b.WriteString(m.renderRow(s, selected, width))
		b.WriteString("\n")
	}

	b.WriteString(lipgloss.NewStyle().Foreground(theme.Current.TextMuted).Render(m.statusMsg))

	return b.String()
}

func (m Model) View() string {
	listW := m.width
	if m.isSplit() {
		listW = m.listWidth()
	}
	listStr := m.renderList(listW)

	if !m.isSplit() {
		return listStr
	}

	panelW := m.width - listW - 1
	divStyle := lipgloss.NewStyle().Foreground(theme.Current.SurfaceVariant)
	divLines := make([]string, m.height)
	for i := range divLines {
		divLines[i] = divStyle.Render("│")
	}
	divider := strings.Join(divLines, "\n")

	sel := m.selectedSession()
	panelStr := sessiondetail.RenderPanel(sel, m.prs, panelW, m.height)

	return lipgloss.JoinHorizontal(lipgloss.Top, listStr, divider, panelStr)
}
