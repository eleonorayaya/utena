package statusview

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/tui/provider"
)

var (
	dimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	activeWinStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	attentionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	workingStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	reviewStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	completedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

type tickMsg time.Time

type Model struct {
	windows            []tmux.Window
	sessions           []session.Session
	claudeSessions     map[string][]claude.ClaudeSession
	currentTmuxSession string
	paneID             string
	expanded           bool
	cursor             int
	width              int
}

func New() Model {
	paneID := os.Getenv("TMUX_PANE")
	var currentSession string
	if paneID != "" {
		out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{session_name}").Output()
		if err == nil {
			currentSession = strings.TrimSpace(string(out))
		}
	}
	return Model{
		paneID:             paneID,
		currentTmuxSession: currentSession,
	}
}

func (m Model) Init() (Model, tea.Cmd) {
	cmds := []tea.Cmd{
		provider.FetchSessions(),
		tick(),
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
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{
			provider.FetchSessions(),
			tick(),
		}
		if m.currentTmuxSession != "" {
			cmds = append(cmds, provider.FetchWindows(m.currentTmuxSession))
		}
		return m, tea.Batch(cmds...)

	case provider.SessionsStateUpdatedMsg:
		m.sessions = msg.Sessions
		m.claudeSessions = msg.ClaudeSessions
		return m, nil

	case provider.WindowsStateUpdatedMsg:
		m.windows = msg.Windows
		return m, nil

	case tea.FocusMsg:
		m.expanded = true
		return m, nil

	case tea.BlurMsg:
		m.expanded = false
		if m.paneID != "" {
			exec.Command("tmux", "resize-pane", "-y", "1", "-t", m.paneID).Run()
			exec.Command("tmux", "select-pane", "-d", "-t", m.paneID).Run()
		}
		return m, nil

	case tea.KeyMsg:
		if m.expanded {
			return m.onKeyMsg(msg)
		}
	}

	return m, nil
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	activeSessions := m.activeSessions()
	switch {
	case key.Matches(msg, keys.Down):
		if m.cursor < len(activeSessions)-1 {
			m.cursor++
		}
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case key.Matches(msg, keys.Select):
		if m.cursor < len(activeSessions) {
			s := activeSessions[m.cursor]
			return m, provider.ActivateSession(s.ID)
		}
		return m, nil
	case key.Matches(msg, keys.Collapse):
		m.expanded = false
		if m.paneID != "" {
			exec.Command("tmux", "resize-pane", "-y", "1", "-t", m.paneID).Run()
			exec.Command("tmux", "select-pane", "-d", "-t", m.paneID).Run()
		}
		return m, nil
	}
	return m, nil
}

func (m Model) activeSessions() []session.Session {
	var result []session.Session
	for _, s := range m.sessions {
		if !s.IsDeleted && !s.IsDead {
			result = append(result, s)
		}
	}
	return result
}

func (m Model) View() string {
	if m.expanded {
		return m.expandedView()
	}
	return m.collapsedView()
}

func (m Model) collapsedView() string {
	var windowParts []string
	for _, w := range m.windows {
		entry := fmt.Sprintf("%d:%s", w.Index, w.Name)
		if w.Active {
			entry += "*"
			entry = activeWinStyle.Render(entry)
		}
		windowParts = append(windowParts, entry)
	}
	windowStr := " " + strings.Join(windowParts, " ")

	var sessionParts []string
	for _, s := range m.activeSessions() {
		name := s.Name
		if name == "" {
			name = s.ID
		}
		indicator := statusIndicator(m.claudeSessions[s.ID])
		if indicator != "" {
			sessionParts = append(sessionParts, name+"("+indicator+")")
		} else {
			sessionParts = append(sessionParts, name)
		}
	}
	sessionStr := strings.Join(sessionParts, " ")

	sep := dimStyle.Render("│")
	return windowStr + " " + sep + " " + sessionStr
}

func (m Model) expandedView() string {
	lines := []string{m.collapsedView()}
	divider := dimStyle.Render(strings.Repeat("─", m.width))
	lines = append(lines, divider)

	activeSessions := m.activeSessions()
	for i, s := range activeSessions {
		name := s.Name
		if name == "" {
			name = s.ID
		}
		prefix := "  "
		if i == m.cursor {
			prefix = cursorStyle.Render("▸ ")
		}
		indicator := statusIndicator(m.claudeSessions[s.ID])
		entry := prefix + name
		if indicator != "" {
			entry += " " + coloredIndicator(m.claudeSessions[s.ID])
		}
		if s.IsAttached {
			entry += dimStyle.Render(" (attached)")
		}
		lines = append(lines, entry)
	}

	helpLine := dimStyle.Render("j/k navigate · enter switch · esc collapse")
	lines = append(lines, helpLine)

	return strings.Join(lines, "\n")
}

func statusIndicator(sessions []claude.ClaudeSession) string {
	if len(sessions) == 0 {
		return ""
	}
	hasNeedsAttention := false
	hasReadyForReview := false
	hasWorking := false
	hasCompleted := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
		case claude.StatusReadyForReview:
			hasReadyForReview = true
		case claude.StatusWorking:
			hasWorking = true
		case claude.StatusCompleted:
			hasCompleted = true
		}
	}
	if hasNeedsAttention {
		return "!"
	}
	if hasWorking {
		return "~"
	}
	if hasReadyForReview {
		return "✓"
	}
	if hasCompleted {
		return "✓"
	}
	return ""
}

func coloredIndicator(sessions []claude.ClaudeSession) string {
	if len(sessions) == 0 {
		return ""
	}
	hasNeedsAttention := false
	hasReadyForReview := false
	hasWorking := false
	hasCompleted := false
	for _, cs := range sessions {
		switch cs.Status {
		case claude.StatusNeedsAttention:
			hasNeedsAttention = true
		case claude.StatusReadyForReview:
			hasReadyForReview = true
		case claude.StatusWorking:
			hasWorking = true
		case claude.StatusCompleted:
			hasCompleted = true
		}
	}
	if hasNeedsAttention {
		return attentionStyle.Render("!")
	}
	if hasWorking {
		return workingStyle.Render("~")
	}
	if hasReadyForReview {
		return reviewStyle.Render("✓")
	}
	if hasCompleted {
		return completedStyle.Render("✓")
	}
	return ""
}
