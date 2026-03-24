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
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tmux"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
)

const collapsedWidth = 14

type tickMsg time.Time

type Model struct {
	sessions           []session.Session
	tabs               map[uint]SessionTab
	windowsBySession   map[string][]tmux.Window
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
		tabs:               make(map[uint]SessionTab),
		windowsBySession:   make(map[string][]tmux.Window),
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
		m.syncTabs()
		return m, nil

	case provider.WindowsStateUpdatedMsg:
		if msg.SessionName != "" {
			m.windowsBySession[msg.SessionName] = msg.Windows
		}
		m.syncTabs()
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

func (m *Model) syncTabs() {
	ordered := m.orderedActiveSessions()
	seen := make(map[uint]bool)
	for i, s := range ordered {
		seen[s.ID] = true
		selected := i == m.cursor && m.focused
		windows := m.windowsBySession[s.TmuxSessionName]
		tab, ok := m.tabs[s.ID]
		if !ok {
			tab = NewSessionTab(s)
		}
		m.tabs[s.ID] = tab.Update(s, windows, selected, m.width)
	}
	for id := range m.tabs {
		if !seen[id] {
			delete(m.tabs, id)
		}
	}
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
		m.syncTabs()
		return m, nil
	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		m.syncTabs()
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
