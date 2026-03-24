package statusview

import (
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
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

const collapsedWidth = 14

var (
	selectedBg = lipgloss.Color("#ffd5d5")

	activeSessionBg   = lipgloss.Color("#f0e8e0")
	inactiveSessionBg = lipgloss.Color("#e8dfd5")

	accentAttentionColor = lipgloss.Color("#e6537a")
	accentWorkingColor   = lipgloss.Color("#6a9bc3")
	accentReviewColor    = lipgloss.Color("#7bb88e")
	accentIdleColor      = lipgloss.Color("#d8cfc5")

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

)

type tickMsg time.Time

type Model struct {
	sessions           []session.Session
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
		for _, s := range m.activeSessions() {
			cmds = append(cmds, provider.FetchWindows(s.TmuxSessionName))
		}
		return m, tea.Batch(cmds...)

	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		return m, nil

	case provider.WindowsStateUpdatedMsg:
		if msg.SessionName != "" {
			m.windowsBySession[msg.SessionName] = msg.Windows
		}
		return m, nil

	case provider.SessionSwitchedMsg:
		m.focusNextPane()
		m.focused = false
		return m, nil

	case tea.KeyMsg:
		if m.isExpanded() {
			return m.onKeyMsg(msg)
		}
	}

	return m, nil
}

func (m Model) focusNextPane() {
	if m.paneID == "" {
		return
	}
	t, err := gotmux.DefaultTmux()
	if err != nil {
		return
	}
	t.Command("select-pane", "-t", "{right-of}")
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

func (m Model) orderedActiveSessions() []session.Session {
	result := m.activeSessions()
	sort.Slice(result, func(i, j int) bool {
		return sessionDisplayName(result[i]) < sessionDisplayName(result[j])
	})
	return result
}

func (m Model) View() string {
	if m.isExpanded() {
		return m.expandedView()
	}
	return m.collapsedView()
}

func (m Model) collapsedView() string {
	maxNameLen := m.width - 2
	if maxNameLen < 1 {
		maxNameLen = 1
	}

	var lines []string
	for _, sess := range m.orderedActiveSessions() {
		name := sessionDisplayName(sess)
		name = truncate(name, maxNameLen)
		lines = append(lines, "  "+name)
	}

	return strings.Join(lines, "\n")
}

func (m Model) expandedView() string {
	innerWidth := m.width
	if innerWidth < 1 {
		innerWidth = 1
	}

	ordered := m.orderedActiveSessions()

	var lines []string
	for i, s := range ordered {
		selected := i == m.cursor && m.focused
		bg := &inactiveSessionBg
		if selected {
			bg = &selectedBg
		} else if s.IsAttached {
			bg = &activeSessionBg
		}
		lines = append(lines, m.emptyLine(innerWidth, bg))
		lines = append(lines, m.renderSessionBlock(s, i, innerWidth)...)
		lines = append(lines, m.renderWindows(s, innerWidth, bg)...)
		lines = append(lines, m.emptyLine(innerWidth, bg))
	}

	if len(ordered) == 0 {
		lines = append(lines, dimStyle.Render("  no active sessions"))
	}

	return strings.Join(lines, "\n")
}

func (m Model) renderSessionBlock(s session.Session, idx, innerWidth int) []string {
	selected := idx == m.cursor && m.focused
	active := s.IsAttached

	bg := &inactiveSessionBg
	if selected {
		bg = &selectedBg
	} else if active {
		bg = &activeSessionBg
	}

	name := sessionDisplayName(s)
	wsName := ""
	if s.Workspace != nil {
		wsName = s.Workspace.Name
	}

	nStyle := sessionNameStyle
	if active {
		nStyle = sessionAttachedStyle
	}
	wsStyle := workspaceStyle
	bStyle, badgeText := claudeBadgeParts(s.ClaudeSessions)

	if bg != nil {
		nStyle = nStyle.Background(*bg)
		wsStyle = wsStyle.Background(*bg)
		if bStyle != nil {
			s := (*bStyle).Background(*bg)
			bStyle = &s
		}
	}

	badgeStr := ""
	if bStyle != nil {
		badgeStr = (*bStyle).Render(badgeText)
	}

	barColor := accentBarColor(s.ClaudeSessions)
	barBase := lipgloss.NewStyle().Foreground(barColor)
	accentWidth := 2

	maxName := innerWidth - accentWidth - lipgloss.Width(badgeStr)
	if badgeStr != "" {
		maxName--
	}
	if maxName < 4 {
		maxName = 4
	}
	truncName := truncate(name, maxName)
	nameStr := nStyle.Render(truncName)

	accent := renderAccent(barBase, bg)
	nameLine := accent + nameStr
	if badgeStr != "" {
		usedWidth := accentWidth + lipgloss.Width(truncName) + lipgloss.Width(badgeStr)
		pad := innerWidth - usedWidth
		if pad < 1 {
			pad = 1
		}
		padStr := strings.Repeat(" ", pad)
		if bg != nil {
			padStr = lipgloss.NewStyle().Background(*bg).Render(padStr)
		}
		nameLine = accent + nameStr + padStr + badgeStr
	}
	nameLine = m.padLine(nameLine, innerWidth, bg)

	var result []string
	result = append(result, nameLine)

	if wsName != "" {
		timeStr := ""
		if !s.LastUsedAt.IsZero() {
			tStyle := dimStyle
			if bg != nil {
				tStyle = tStyle.Background(*bg)
			}
			timeStr = tStyle.Render(" · " + common.TimeAgo(s.LastUsedAt))
		}
		wsLine := accent + wsStyle.Render(wsName) + timeStr
		wsLine = m.padLine(wsLine, innerWidth, bg)
		result = append(result, wsLine)
	}

	return result
}

func (m Model) padLine(line string, width int, bg *lipgloss.Color) string {
	if bg == nil {
		return line
	}
	lineWidth := lipgloss.Width(line)
	if lineWidth < width {
		line += lipgloss.NewStyle().Background(*bg).Render(strings.Repeat(" ", width-lineWidth))
	}
	return line
}

func (m Model) emptyLine(width int, bg *lipgloss.Color) string {
	if bg != nil {
		return lipgloss.NewStyle().Background(*bg).Render(strings.Repeat(" ", width))
	}
	return ""
}

func renderAccent(barBase lipgloss.Style, bg *lipgloss.Color) string {
	if bg != nil {
		return barBase.Background(*bg).Render("▐") + lipgloss.NewStyle().Background(*bg).Render(" ")
	}
	return barBase.Render("▐") + " "
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
	case claude.StatusDone:
		s := completedDotStyle
		return &s, " ✓ "
	default:
		return nil, ""
	}
}

func (m Model) renderWindows(s session.Session, innerWidth int, bg *lipgloss.Color) []string {
	windows := m.windowsBySession[s.TmuxSessionName]
	if len(windows) == 0 {
		return nil
	}

	barColor := accentBarColor(s.ClaudeSessions)
	barBase := lipgloss.NewStyle().Foreground(barColor)
	accent := renderAccent(barBase, bg)

	var lines []string
	for _, w := range windows {
		marker := "  "
		wStyle := windowDimStyle
		if w.Active {
			marker = "› "
			wStyle = windowActiveStyle
		}
		if bg != nil {
			wStyle = wStyle.Background(*bg)
		}
		line := accent + wStyle.Render(marker+w.Name)
		line = m.padLine(line, innerWidth, bg)
		lines = append(lines, line)
	}
	return lines
}

func accentBarColor(claudeSessions []claude.ClaudeSession) lipgloss.Color {
	status := aggregateStatus(claudeSessions)
	switch status {
	case claude.StatusNeedsAttention:
		return accentAttentionColor
	case claude.StatusWorking:
		return accentWorkingColor
	case claude.StatusReadyForReview:
		return accentReviewColor
	default:
		return accentIdleColor
	}
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

func aggregateStatus(sessions []claude.ClaudeSession) claude.ClaudeSessionStatus {
	return claude.AggregateStatus(sessions)
}
