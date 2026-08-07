package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type rowKind int

const (
	rowSession rowKind = iota
	rowCheckout
)

type row struct {
	kind     rowKind
	session  *Session
	checkout *Checkout
	status   string
	branch   string
	dirty    int
	last     bool
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Focus   key.Binding
	Archive key.Binding
	Delete  key.Binding
	New     key.Binding
	Refresh key.Binding
	Help    key.Binding
	Quit    key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:      key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:    key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Focus:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus")),
		Archive: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
		Delete:  key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Focus, k.New, k.Archive, k.Delete, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Focus},
		{k.New, k.Archive, k.Delete},
		{k.Refresh},
		{k.Help, k.Quit},
	}
}

type sidebar struct {
	herdr   *herdrClient
	rows    []row
	cursor  int
	offset  int
	width   int
	height  int
	keys    keyMap
	help    help.Model
	status  string
	pending string
	err     error
	events  <-chan string
	live    bool

	mode        mode
	createKeys  createKeyMap
	repos       []repoChoice
	repoCursor  int
	branchInput textinput.Model
}

type reloadedMsg struct {
	rows []row
	err  error
}

type tickMsg time.Time

type eventMsg string

type streamStartedMsg struct{ ch <-chan string }

type streamClosedMsg struct{}

type reconnectMsg struct{}

func keyPress(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newSidebar(h *herdrClient) sidebar {
	return sidebar{herdr: h, keys: newKeyMap(), createKeys: newCreateKeyMap(),
		help: help.New(), width: 48, height: 24}
}

var subscribedEvents = []string{
	"workspace.created",
	"workspace.updated",
	"workspace.closed",
	"workspace.metadata_updated",
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m sidebar) listenCmd() tea.Cmd {
	if m.events == nil {
		return nil
	}
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return streamClosedMsg{}
		}
		return eventMsg(ev)
	}
}

func startStream(h *herdrClient) tea.Cmd {
	return func() tea.Msg {
		ch, err := h.subscribe(subscribedEvents)
		if err != nil {
			return streamClosedMsg{}
		}
		return streamStartedMsg{ch: ch}
	}
}

func (m sidebar) reload() tea.Cmd {
	h := m.herdr
	return func() tea.Msg {
		state, err := loadState()
		if err != nil {
			return reloadedMsg{err: err}
		}
		live, err := h.listWorkspaces()
		if err != nil {
			return reloadedMsg{err: err}
		}
		var rows []row
		for i := range state.Sessions {
			s := &state.Sessions[i]
			if s.Status == statusArchived {
				continue
			}
			rows = append(rows, row{kind: rowSession, session: s, status: live[s.WorkspaceID].AgentStatus})
			for j := range s.Checkouts {
				c := &s.Checkouts[j]
				br, dirty := checkoutState(c.Path)
				rows = append(rows, row{
					kind:     rowCheckout,
					session:  s,
					checkout: c,
					status:   live[c.WorkspaceID].AgentStatus,
					branch:   br,
					dirty:    dirty,
					last:     j == len(s.Checkouts)-1,
				})
			}
		}
		return reloadedMsg{rows: rows}
	}
}

type gitSnapshot struct {
	branch string
	dirty  int
	at     time.Time
}

var (
	gitCacheMu  sync.Mutex
	gitCache    = map[string]gitSnapshot{}
	gitCacheTTL = 5 * time.Second
)

func checkoutState(path string) (string, int) {
	gitCacheMu.Lock()
	if snap, ok := gitCache[path]; ok && time.Since(snap.at) < gitCacheTTL {
		gitCacheMu.Unlock()
		return snap.branch, snap.dirty
	}
	gitCacheMu.Unlock()

	branch, dirty := readCheckoutState(path)

	gitCacheMu.Lock()
	gitCache[path] = gitSnapshot{branch: branch, dirty: dirty, at: time.Now()}
	gitCacheMu.Unlock()
	return branch, dirty
}

func readCheckoutState(path string) (string, int) {
	if _, err := os.Stat(path); err != nil {
		return "missing", 0
	}
	branch, err := git(path, "branch", "--show-current")
	if err != nil {
		return "?", 0
	}
	out, err := git(path, "status", "--porcelain")
	if err != nil || strings.TrimSpace(out) == "" {
		return branch, 0
	}
	return branch, len(strings.Split(strings.TrimSpace(out), "\n"))
}

func (m sidebar) Init() tea.Cmd {
	return tea.Batch(m.reload(), tick(), startStream(m.herdr))
}

func (m sidebar) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.Width = msg.Width
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.reload(), tick())

	case streamStartedMsg:
		m.events = msg.ch
		m.live = true
		return m, m.listenCmd()

	case eventMsg:
		return m, tea.Batch(m.reload(), m.listenCmd())

	case streamClosedMsg:
		m.events = nil
		m.live = false
		return m, tea.Tick(5*time.Second, func(time.Time) tea.Msg { return reconnectMsg{} })

	case reconnectMsg:
		return m, startStream(m.herdr)

	case reloadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.rows = msg.rows
		m.clamp()
		return m, nil

	case createdMsg:
		if msg.err != nil {
			m.err = msg.err
			m.status = ""
			return m, nil
		}
		m.status = "created " + msg.name
		return m, m.reload()

	case tea.KeyMsg:
		switch m.mode {
		case modePickRepos:
			return m.updatePickRepos(msg)
		case modeBranch:
			return m.updateBranch(msg)
		}
		return m.onKey(msg)
	}

	if m.mode == modeBranch {
		return m.updateBranch(msg)
	}
	return m, nil
}

func (m sidebar) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if !key.Matches(msg, m.keys.Archive, m.keys.Delete) {
		m.pending = ""
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll

	case key.Matches(msg, m.keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.clamp()
		}

	case key.Matches(msg, m.keys.Down):
		if m.cursor < len(m.rows)-1 {
			m.cursor++
			m.clamp()
		}

	case key.Matches(msg, m.keys.New):
		return m.startCreate(), nil

	case key.Matches(msg, m.keys.Refresh):
		m.status = "refreshing…"
		return m, m.reload()

	case key.Matches(msg, m.keys.Focus):
		r, ok := m.current()
		if !ok {
			break
		}
		id := r.session.WorkspaceID
		if r.kind == rowCheckout {
			id = r.checkout.WorkspaceID
		}
		if err := m.herdr.focusWorkspace(id); err != nil {
			m.err = err
			break
		}
		return m, tea.Quit

	case key.Matches(msg, m.keys.Archive):
		r, ok := m.current()
		if !ok || r.session == nil {
			break
		}
		name := r.session.Name
		if m.pending != "archive:"+name {
			m.pending = "archive:" + name
			m.status = fmt.Sprintf("press a again to archive %s", name)
			break
		}
		m.pending = ""
		if err := m.archive(name); err != nil {
			m.err = err
			break
		}
		m.status = "archived " + name
		return m, m.reload()

	case key.Matches(msg, m.keys.Delete):
		r, ok := m.current()
		if !ok || r.session == nil {
			break
		}
		name := r.session.Name
		if m.pending != "delete:"+name {
			m.pending = "delete:" + name
			m.status = fmt.Sprintf("press d again to DELETE %s (worktrees removed)", name)
			break
		}
		m.pending = ""
		if err := m.remove(name); err != nil {
			m.err = err
			break
		}
		m.status = "deleted " + name
		return m, m.reload()
	}
	return m, nil
}

func (m sidebar) archive(name string) error { return archiveSession(m.herdr, name) }

func (m sidebar) remove(name string) error { return deleteSession(m.herdr, name) }

func (m sidebar) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m *sidebar) clamp() {
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	body := m.bodyHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+body {
		m.offset = m.cursor - body + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

func (m sidebar) bodyHeight() int {
	h := m.height - 3
	if h < 1 {
		return 1
	}
	return h
}

func statusGlyph(s string) (string, lipgloss.Color) {
	switch s {
	case "blocked":
		return "!", theme.Current.Error
	case "working":
		return "●", theme.Current.StatusActive
	case "done":
		return "✓", theme.Current.StatusReady
	case "idle":
		return "·", theme.Current.TextMuted
	default:
		return " ", theme.Current.TextMuted
	}
}

func (m sidebar) View() string {
	switch m.mode {
	case modePickRepos:
		return m.viewPickRepos()
	case modeBranch:
		return m.viewBranch()
	}
	t := theme.Current
	var b strings.Builder

	title := lipgloss.NewStyle().Foreground(t.TextOnPrimary).Background(t.Primary).Bold(true).
		Width(m.width).Padding(0, 1).Render("sessions")
	b.WriteString(title + "\n")

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Error).Render("error: "+m.err.Error()) + "\n")
	}

	if len(m.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(1, 1).
			Render("no sessions\n\nherdr-utena new -branch <b> -repo <path> …") + "\n")
	}

	body := m.bodyHeight()
	for i := m.offset; i < len(m.rows) && i < m.offset+body; i++ {
		b.WriteString(m.renderRow(m.rows[i], i == m.cursor) + "\n")
	}

	foot := m.status
	if foot == "" {
		foot = m.help.View(m.keys)
	}
	b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).Render(foot))
	return b.String()
}

func (m sidebar) renderRow(r row, selected bool) string {
	t := theme.Current
	glyph, gc := statusGlyph(r.status)

	var line string
	switch r.kind {
	case rowSession:
		name := lipgloss.NewStyle().Foreground(t.TextEmphasis).Bold(true).Render(r.session.Name)
		count := lipgloss.NewStyle().Foreground(t.TextMuted).
			Render(fmt.Sprintf(" (%d)", len(r.session.Checkouts)))
		line = fmt.Sprintf("%s %s%s", lipgloss.NewStyle().Foreground(gc).Render(glyph), name, count)
	case rowCheckout:
		branch := lipgloss.NewStyle().Foreground(t.AccentBlue).Render(r.branch)
		dirty := ""
		if r.dirty > 0 {
			dirty = lipgloss.NewStyle().Foreground(t.StatusPending).Render(fmt.Sprintf(" ●%d", r.dirty))
		}
		tree := "├─"
		if r.last {
			tree = "└─"
		}
		line = fmt.Sprintf("%s %s %s %s%s",
			lipgloss.NewStyle().Foreground(t.TextMuted).Render(tree),
			lipgloss.NewStyle().Foreground(gc).Render(glyph),
			lipgloss.NewStyle().Foreground(t.Text).Render(filepath.Base(r.checkout.Label)),
			branch, dirty)
	}

	style := lipgloss.NewStyle().Width(m.width).Padding(0, 1)
	if selected {
		style = style.Background(t.Selection)
	}
	return style.Render(line)
}
