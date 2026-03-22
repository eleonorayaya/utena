package statusview

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/GianlucaP106/gotmux/gotmux"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

const collapsedWidth = 14

var (
	selectedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#edd5d0"))

	workspaceStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9370b9"))

	cursorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#e6537a")).
			Bold(true)

	sessionNameStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#4a4a4a"))

	sessionAttachedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#2a2a2a")).
				Bold(true)

	windowActiveStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6a9bc3"))

	windowDimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8a7873"))

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8a7873"))

	attentionDotStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#e6537a")).
				Bold(true)

	workingDotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d97c6e"))

	reviewDotStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#5fafa5"))

	completedDotStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#8a7873"))

	attentionBadgeStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#fef6f0")).
				Background(lipgloss.Color("#d16577"))

	workingBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#d97c6e"))

	reviewBadgeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#5fafa5"))

	dividerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#d8cfc5"))
)

type tickMsg time.Time

type Model struct {
	sessions           []session.Session
	claudeSessions     map[string][]claude.ClaudeSession
	windowsBySession   map[string][]tmux.Window
	expandedSessions   map[string]bool
	currentTmuxSession string
	paneID             string
	focused            bool
	cursor             int
	width              int
	height             int
}

func New() Model {
	paneID := os.Getenv("TMUX_PANE")
	var currentSession string
	if paneID != "" {
		t, err := gotmux.DefaultTmux()
		if err == nil {
			output, err := t.Command("display-message", "-p", "-t", paneID, "#{session_name}")
			if err == nil {
				currentSession = strings.TrimSpace(output)
			}
		}
	}
	return Model{
		paneID:             paneID,
		currentTmuxSession: currentSession,
		windowsBySession:   make(map[string][]tmux.Window),
		expandedSessions:   map[string]bool{currentSession: true},
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	cmds := []tea.Cmd{
		provider.FetchSessions(),
		tick(),
		router.SetHelpVisible(false),
	}
	if m.currentTmuxSession != "" {
		cmds = append(cmds, provider.FetchWindows(m.currentTmuxSession))
	}
	return m, tea.Batch(cmds...)
}

func (m Model) Keys() help.KeyMap {
	return keys
}

func tick() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) isExpanded() bool {
	return m.width > collapsedWidth
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		wasExpanded := m.isExpanded()
		m.width = msg.Width
		m.height = msg.Height
		nowExpanded := m.isExpanded()
		if wasExpanded != nowExpanded {
			return m, tea.ClearScreen
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, nil

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{
			provider.FetchSessions(),
			tick(),
		}
		for name, expanded := range m.expandedSessions {
			if expanded {
				cmds = append(cmds, provider.FetchWindows(name))
			}
		}
		for _, s := range m.activeSessions() {
			if s.IsAttached {
				cmds = append(cmds, provider.FetchWindows(s.TmuxSessionName))
				break
			}
		}
		return m, tea.Batch(cmds...)

	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		m.claudeSessions = msg.ClaudeSessions
		return m, nil

	case provider.WindowsStateUpdatedMsg:
		if msg.SessionName != "" {
			m.windowsBySession[msg.SessionName] = msg.Windows
		}
		return m, nil

	case tea.KeyMsg:
		if m.isExpanded() {
			return m.onKeyMsg(msg)
		}
	}

	return m, nil
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	ordered := m.orderedActiveSessions()
	switch {
	case key.Matches(msg, keys.Down):
		if m.cursor < len(ordered)-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case key.Matches(msg, keys.Select):
		if m.cursor < len(ordered) {
			s := ordered[m.cursor]
			if s.IsAttached {
				return m, nil
			}
			return m, provider.ActivateSession(s.ID)
		}
		return m, nil
	case key.Matches(msg, keys.Toggle):
		if m.cursor < len(ordered) {
			s := ordered[m.cursor]
			if s.IsAttached {
				return m, nil
			}
			tmuxName := s.TmuxSessionName
			if m.expandedSessions[tmuxName] {
				delete(m.expandedSessions, tmuxName)
			} else {
				m.expandedSessions[tmuxName] = true
				return m, provider.FetchWindows(tmuxName)
			}
		}
		return m, nil
	case key.Matches(msg, keys.Collapse):
		return m, nil
	}
	return m, nil
}

func (m Model) activeSessions() []session.Session {
	var result []session.Session
	for _, s := range m.sessions {
		if s.Status == session.StatusReady || s.Status == session.StatusCreating {
			result = append(result, s)
		}
	}
	return result
}

func (m Model) categorizedSessions() (active, others []session.Session) {
	for _, s := range m.activeSessions() {
		if s.IsAttached {
			active = append(active, s)
		} else {
			others = append(others, s)
		}
	}
	sortByName := func(ss []session.Session) {
		sort.Slice(ss, func(i, j int) bool {
			return sessionDisplayName(ss[i]) < sessionDisplayName(ss[j])
		})
	}
	sortByName(active)
	sortByName(others)
	return
}

func (m Model) orderedActiveSessions() []session.Session {
	active, others := m.categorizedSessions()
	var result []session.Session
	result = append(result, others...)
	result = append(result, active...)
	return result
}

func (m Model) showWindows(s session.Session) bool {
	return s.IsAttached || m.expandedSessions[s.TmuxSessionName]
}

func (m Model) View() string {
	if m.isExpanded() {
		return m.expandedView()
	}
	return m.collapsedView()
}

func (m Model) collapsedView() string {
	maxNameLen := m.width - 3
	if maxNameLen < 1 {
		maxNameLen = 1
	}

	var lines []string
	for _, sess := range m.orderedActiveSessions() {
		name := sessionDisplayName(sess)
		name = truncate(name, maxNameLen)
		dot := statusDot(m.claudeSessions[sess.TmuxSessionName], sess.IsAttached)
		lines = append(lines, " "+dot+" "+name)
	}

	return bottomAlign(lines, m.height)
}

func (m Model) expandedView() string {
	innerWidth := m.width
	if innerWidth < 1 {
		innerWidth = 1
	}

	active, others := m.categorizedSessions()
	ordered := m.orderedActiveSessions()

	var lines []string
	cursorIdx := 0

	for i, s := range others {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, m.renderSessionRow(s, cursorIdx, innerWidth, ordered))
		if m.showWindows(s) {
			lines = append(lines, m.renderWindows(s, innerWidth)...)
		}
		cursorIdx++
	}

	if len(others) > 0 && len(active) > 0 {
		lines = append(lines, "")
		lines = append(lines, dividerStyle.Render(strings.Repeat("─", innerWidth)))
		lines = append(lines, "")
	}

	for _, s := range active {
		lines = append(lines, m.renderSessionRow(s, cursorIdx, innerWidth, ordered))
		lines = append(lines, m.renderWindows(s, innerWidth)...)
		lines = append(lines, "")
		cursorIdx++
	}

	if len(ordered) == 0 {
		lines = append(lines, dimStyle.Render("  no active sessions"))
	}

	return bottomAlign(lines, m.height)
}

func (m Model) renderSessionRow(s session.Session, idx, innerWidth int, _ []session.Session) string {
	selected := idx == m.cursor && m.focused
	bg := lipgloss.Color("#edd5d0")

	name := sessionDisplayName(s)
	wsName := ""
	if s.Workspace != nil {
		wsName = s.Workspace.Name
	}

	dotStyle := statusDotStyle(m.claudeSessions[s.TmuxSessionName], s.IsAttached)
	dotChar := statusDotChar(m.claudeSessions[s.TmuxSessionName], s.IsAttached)

	nStyle := sessionNameStyle
	if s.IsAttached {
		nStyle = sessionAttachedStyle
	}
	wsStyle := workspaceStyle
	bStyle, badgeText := claudeBadgeParts(m.claudeSessions[s.TmuxSessionName])

	if selected {
		dotStyle = dotStyle.Background(bg)
		nStyle = nStyle.Background(bg)
		wsStyle = wsStyle.Background(bg)
		if bStyle != nil {
			s := (*bStyle).Background(bg)
			bStyle = &s
		}
	}

	sp := " "
	if selected {
		sp = lipgloss.NewStyle().Background(bg).Render(" ")
	}

	prefix := sp + dotStyle.Render(dotChar) + sp

	wsStr := ""
	if wsName != "" {
		wsStr = sp + wsStyle.Render(wsName)
	}

	badgeStr := ""
	if bStyle != nil {
		badgeStr = (*bStyle).Render(badgeText)
	}

	maxName := innerWidth - lipgloss.Width(prefix) - lipgloss.Width(wsStr) - lipgloss.Width(badgeStr) - 1
	if maxName < 4 {
		maxName = 4
	}
	truncName := truncate(name, maxName)
	nameStr := nStyle.Render(truncName)

	line := prefix + nameStr + wsStr
	if badgeStr != "" {
		usedWidth := lipgloss.Width(prefix + truncName + wsStr + badgeStr)
		pad := innerWidth - usedWidth
		if pad < 1 {
			pad = 1
		}
		if selected {
			line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad)) + badgeStr
		} else {
			line += strings.Repeat(" ", pad) + badgeStr
		}
	}

	if selected {
		lineWidth := lipgloss.Width(line)
		if lineWidth < innerWidth {
			line += lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", innerWidth-lineWidth))
		}
	}

	return line
}

func statusDotChar(claudeSessions []claude.ClaudeSession, attached bool) string {
	status := aggregateStatus(claudeSessions)
	switch status {
	case claude.StatusNeedsAttention:
		return "◆"
	case claude.StatusWorking, claude.StatusReadyForReview, claude.StatusCompleted:
		return "●"
	default:
		if attached {
			return "●"
		}
		return "○"
	}
}

func statusDotStyle(claudeSessions []claude.ClaudeSession, attached bool) lipgloss.Style {
	status := aggregateStatus(claudeSessions)
	switch status {
	case claude.StatusNeedsAttention:
		return attentionDotStyle
	case claude.StatusWorking:
		return workingDotStyle
	case claude.StatusReadyForReview:
		return reviewDotStyle
	case claude.StatusCompleted:
		return completedDotStyle
	default:
		if attached {
			return sessionAttachedStyle
		}
		return dimStyle
	}
}

func claudeBadgeParts(sessions []claude.ClaudeSession) (*lipgloss.Style, string) {
	status := aggregateStatus(sessions)
	switch status {
	case claude.StatusNeedsAttention:
		s := attentionBadgeStyle
		return &s, " ! "
	case claude.StatusWorking:
		s := workingBadgeStyle
		return &s, " ~ "
	case claude.StatusReadyForReview:
		s := reviewBadgeStyle
		return &s, " ✓ "
	case claude.StatusCompleted:
		s := completedDotStyle
		return &s, " ✓ "
	default:
		return nil, ""
	}
}

func (m Model) renderWindows(s session.Session, innerWidth int) []string {
	windows := m.windowsBySession[s.TmuxSessionName]
	if len(windows) == 0 {
		return nil
	}

	var lines []string
	for _, w := range windows {
		winEntry := fmt.Sprintf("     %d:%s", w.Index, w.Name)
		if w.Active {
			lines = append(lines, windowActiveStyle.Render(winEntry))
		} else {
			lines = append(lines, windowDimStyle.Render(winEntry))
		}
	}
	return lines
}

func bottomAlign(lines []string, height int) string {
	if height > len(lines) {
		padding := make([]string, height-len(lines))
		for i := range padding {
			padding[i] = ""
		}
		lines = append(padding, lines...)
	}
	return strings.Join(lines, "\n")
}

func sessionDisplayName(s session.Session) string {
	if s.Name != "" {
		return s.Name
	}
	return s.TmuxSessionName
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 1 {
		return s[:maxLen]
	}
	return s[:maxLen-1] + "…"
}

func statusDot(claudeSessions []claude.ClaudeSession, attached bool) string {
	status := aggregateStatus(claudeSessions)
	switch status {
	case claude.StatusNeedsAttention:
		return attentionDotStyle.Render("◆")
	case claude.StatusWorking:
		return workingDotStyle.Render("●")
	case claude.StatusReadyForReview:
		return reviewDotStyle.Render("●")
	case claude.StatusCompleted:
		return completedDotStyle.Render("●")
	default:
		if attached {
			return sessionAttachedStyle.Render("●")
		}
		return dimStyle.Render("○")
	}
}

func claudeBadge(sessions []claude.ClaudeSession) string {
	status := aggregateStatus(sessions)
	switch status {
	case claude.StatusNeedsAttention:
		return attentionBadgeStyle.Render(" ! ")
	case claude.StatusWorking:
		return workingBadgeStyle.Render(" ~ ")
	case claude.StatusReadyForReview:
		return reviewBadgeStyle.Render(" ✓ ")
	case claude.StatusCompleted:
		return completedDotStyle.Render(" ✓ ")
	default:
		return ""
	}
}

func aggregateStatus(sessions []claude.ClaudeSession) claude.ClaudeSessionStatus {
	if len(sessions) == 0 {
		return ""
	}
	hasNeedsAttention := false
	hasWorking := false
	hasReadyForReview := false
	hasCompleted := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
		case claude.StatusWorking:
			hasWorking = true
		case claude.StatusReadyForReview:
			hasReadyForReview = true
		case claude.StatusCompleted:
			hasCompleted = true
		}
	}
	if hasNeedsAttention {
		return claude.StatusNeedsAttention
	}
	if hasWorking {
		return claude.StatusWorking
	}
	if hasReadyForReview {
		return claude.StatusReadyForReview
	}
	if hasCompleted {
		return claude.StatusCompleted
	}
	return ""
}
