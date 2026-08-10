package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type rowKind int

const (
	rowSession rowKind = iota
	rowCheckout
	rowHeading
	rowWorkspace
)

type row struct {
	kind      rowKind
	expanded  bool
	heading   string
	workspace *liveWorkspace
	session   *Session
	checkout  *Checkout
	status    string
	branch    string
	dirty     int
	last      bool
}

type keyMap struct {
	Up             key.Binding
	Down           key.Binding
	Focus          key.Binding
	Archive        key.Binding
	Delete         key.Binding
	New            key.Binding
	Expand         key.Binding
	Collapse       key.Binding
	Filter         key.Binding
	ToggleArchived key.Binding
	Refresh        key.Binding
	Help           key.Binding
	Quit           key.Binding
}

func newKeyMap() keyMap {
	return keyMap{
		Up:             key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("k/↑", "up")),
		Down:           key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/↓", "down")),
		Focus:          key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus")),
		Archive:        key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "archive")),
		Delete:         key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "delete")),
		New:            key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
		Expand:         key.NewBinding(key.WithKeys(" ", "l", "right"), key.WithHelp("space", "expand")),
		Collapse:       key.NewBinding(key.WithKeys("h", "left"), key.WithHelp("h", "collapse")),
		Filter:         key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		ToggleArchived: key.NewBinding(key.WithKeys("."), key.WithHelp(".", "archived")),
		Refresh:        key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
		Help:           key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:           key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"), key.WithHelp("q", "quit")),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Expand, k.Filter, k.Focus, k.New, k.Archive, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Focus},
		{k.Expand, k.Collapse, k.Filter},
		{k.New, k.Archive, k.Delete},
		{k.ToggleArchived},
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

	showArchived bool
	hiddenCount  int
	visible      []int
	terms        []string
	filtering    bool
	input        textinput.Model
	popup        bool
	expanded     map[string]bool
	dirty        map[string]int
	mode         mode
	createKeys   createKeyMap
	repos        []repoChoice
	repoCursor   int
	picked       []string
	pickingFor   int
	branches     []string
	branchCursor int
	branchInput  textinput.Model
	branchErr    string
	pendingCheck string
	nameInput    textinput.Model
	nameErr      string
}

type reloadedMsg struct {
	rows   []row
	hidden int
	err    error
}

type tickMsg time.Time

type eventMsg string

type streamStartedMsg struct{ ch <-chan string }

type streamClosedMsg struct{}

type reconnectMsg struct{}

type dirtyMsg struct {
	path  string
	dirty int
}

func dirtyCmd(path string) tea.Cmd {
	return func() tea.Msg {
		out, err := git(path, "status", "--porcelain")
		if err != nil {
			return dirtyMsg{path: path, dirty: -1}
		}
		out = strings.TrimSpace(out)
		if out == "" {
			return dirtyMsg{path: path, dirty: 0}
		}
		return dirtyMsg{path: path, dirty: len(strings.Split(out, "\n"))}
	}
}

func (m sidebar) dirtyForCursor() tea.Cmd {
	r, ok := m.current()
	if !ok || r.kind != rowCheckout || r.checkout == nil {
		return nil
	}
	if _, done := m.dirty[r.checkout.Path]; done {
		return nil
	}
	return dirtyCmd(r.checkout.Path)
}

func themedHelp() help.Model {
	h := help.New()
	t := theme.Current
	key := lipgloss.NewStyle().Foreground(t.TextEmphasis)
	desc := lipgloss.NewStyle().Foreground(t.Text)
	sep := lipgloss.NewStyle().Foreground(t.TextMuted)
	h.Styles.ShortKey, h.Styles.FullKey = key, key
	h.Styles.ShortDesc, h.Styles.FullDesc = desc, desc
	h.Styles.ShortSeparator, h.Styles.FullSeparator = sep, sep
	return h
}

func filterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "/"
	ti.Placeholder = "filter"
	return ti
}

func keyPress(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func newSidebar(h *herdrClient) sidebar {
	return sidebar{herdr: h, keys: newKeyMap(), createKeys: newCreateKeyMap(),
		dirty: map[string]int{}, expanded: map[string]bool{}, input: filterInput(),
		help: themedHelp(), width: 48, height: 24}
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
	showArchived := m.showArchived
	expanded := m.expanded
	return func() tea.Msg {
		sessions, ungrouped, err := loadSessions(h)
		if err != nil {
			return reloadedMsg{err: err}
		}
		var rows []row
		archivedHidden := 0
		for i := range sessions {
			s := &sessions[i]
			if s.Hidden() {
				archivedHidden++
				if !showArchived {
					continue
				}
			}
			rows = append(rows, row{kind: rowSession, session: s, status: s.AgentStatus,
				expanded: expanded[s.Name]})
			for j := range s.Checkouts {
				c := &s.Checkouts[j]
				rows = append(rows, row{
					kind:     rowCheckout,
					session:  s,
					checkout: c,
					status:   c.AgentStatus,
					branch:   c.Branch,
					dirty:    c.Dirty,
					last:     j == len(s.Checkouts)-1,
				})
			}
		}
		if len(ungrouped) > 0 {
			rows = append(rows, row{kind: rowHeading, heading: "other workspaces"})
			for i := range ungrouped {
				w := ungrouped[i]
				rows = append(rows, row{kind: rowWorkspace, workspace: &w, status: w.AgentStatus})
			}
		}
		return reloadedMsg{rows: rows, hidden: archivedHidden}
	}
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

	case dirtyMsg:
		if m.dirty == nil {
			m.dirty = map[string]int{}
		}
		m.dirty[msg.path] = msg.dirty
		return m, nil

	case reloadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		m.hiddenCount = msg.hidden
		want := m.cursorKey()
		m.rows = msg.rows
		m.applyVisible()
		if want != "" {
			found := false
			for i, idx := range m.visible {
				if rowKey(m.rows[idx]) == want {
					m.cursor, found = i, true
					break
				}
			}
			if !found {
				m.cursor, m.offset = 0, 0
			}
		}
		m.clamp()
		return m, nil

	case activatedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.status = ""
			return m, nil
		}
		m.status = ""
		return m, m.reload()

	case branchesMsg:
		if msg.repo == m.currentPickRepo() {
			m.branches = msg.branches
			m.branchCursor = 0
			m.status = ""
		}
		return m, nil

	case branchCheckedMsg:
		return m.onBranchChecked(msg)

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
		case modeBranchPick:
			return m.updateBranchPick(msg)
		case modeBranchManual:
			return m.updateBranchManual(msg)
		case modeBranchMode:
			return m.updateBranchMode(msg)
		case modeName:
			return m.updateName(msg)
		}
		return m.onKey(msg)
	}

	switch m.mode {
	case modeBranchManual:
		return m.updateBranchManual(msg)
	case modeName:
		return m.updateName(msg)
	}
	return m, nil
}

func (m sidebar) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch {
		case key.Matches(msg, m.keys.Quit):
			m.filtering = false
			m.input.Blur()
			m.input.SetValue("")
			m.applyVisible()
			return m, nil
		case key.Matches(msg, m.keys.Focus):
			return m.activateCursor()
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		m.applyVisible()
		return m, cmd
	}

	if !key.Matches(msg, m.keys.Archive, m.keys.Delete) {
		m.pending = ""
	}

	switch {
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll

	case key.Matches(msg, m.keys.Up):
		for i := m.cursor - 1; i >= 0; i-- {
			if selectableRow(m.rows[m.visible[i]]) {
				m.cursor = i
				m.clamp()
				break
			}
		}

	case key.Matches(msg, m.keys.Down):
		for i := m.cursor + 1; i < len(m.visible); i++ {
			if selectableRow(m.rows[m.visible[i]]) {
				m.cursor = i
				m.clamp()
				break
			}
		}

	case key.Matches(msg, m.keys.Expand):
		if r, ok := m.current(); ok && r.session != nil && len(r.session.Checkouts) > 0 {
			m.expanded[r.session.Name] = !m.expanded[r.session.Name]
			m.applyVisible()
			return m, nil
		}

	case key.Matches(msg, m.keys.Collapse):
		if r, ok := m.current(); ok && r.session != nil {
			if r.kind == rowCheckout || m.expanded[r.session.Name] {
				m.expanded[r.session.Name] = false
				m.applyVisible()
				return m, nil
			}
		}

	case key.Matches(msg, m.keys.New):
		return m.startCreate(), nil

	case key.Matches(msg, m.keys.ToggleArchived):
		m.showArchived = !m.showArchived
		switch {
		case !m.showArchived:
			m.status = ""
		case m.hiddenCount == 0:
			m.status = "no hidden sessions"
		default:
			m.status = fmt.Sprintf("showing %d hidden", m.hiddenCount)
		}
		return m, m.reload()

	case key.Matches(msg, m.keys.Refresh):
		m.status = "refreshing…"
		return m, m.reload()

	case key.Matches(msg, m.keys.Focus):
		return m.activateCursor()

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
			m.status = fmt.Sprintf("press d again to delete %s", name)
			break
		}
		m.pending = ""
		if err := m.remove(name); err != nil {
			m.err = err
			break
		}
		m.status = "deleted " + name + " · worktrees removed later"
		return m, m.reload()
	}
	return m, m.dirtyForCursor()
}

func (m sidebar) activateCursor() (tea.Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if r.kind == rowWorkspace {
		if err := m.herdr.focusWorkspace(r.workspace.ID); err != nil {
			m.err = err
			return m, nil
		}
		return m, tea.Quit
	}
	if r.session == nil {
		return m, nil
	}
	if !r.session.Active() {
		name := r.session.Name
		m.status = "opening " + name + "…"
		return m, m.activateCmd(name)
	}
	id := r.session.WorkspaceID
	if r.kind == rowCheckout {
		id = r.checkout.WorkspaceID
	}
	if id == "" {
		return m, nil
	}
	if err := m.herdr.focusWorkspace(id); err != nil {
		m.err = err
		return m, nil
	}
	return m, tea.Quit
}

func (m sidebar) archive(name string) error { return archiveSession(m.herdr, name) }

func (m sidebar) remove(name string) error { return deleteSession(m.herdr, name) }

func (m *sidebar) applyVisible() {
	m.terms = m.terms[:0]
	for _, r := range m.rows {
		m.terms = append(m.terms, rowTerm(r))
	}
	m.visible = m.visible[:0]

	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		for i, r := range m.rows {
			if r.kind == rowCheckout && !m.expanded[r.session.Name] {
				continue
			}
			m.visible = append(m.visible, i)
		}
	} else {
		for _, match := range fuzzy.Find(q, m.terms) {
			if selectableRow(m.rows[match.Index]) {
				m.visible = append(m.visible, match.Index)
			}
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.snapToSelectable()
	m.clamp()
}

func (m *sidebar) snapToSelectable() {
	if m.cursor < len(m.visible) && selectableRow(m.rows[m.visible[m.cursor]]) {
		return
	}
	for i := m.cursor; i < len(m.visible); i++ {
		if selectableRow(m.rows[m.visible[i]]) {
			m.cursor = i
			return
		}
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if selectableRow(m.rows[m.visible[i]]) {
			m.cursor = i
			return
		}
	}
}

func rowKey(r row) string {
	switch r.kind {
	case rowSession:
		return "s:" + r.session.Name
	case rowCheckout:
		return "c:" + r.checkout.Path
	case rowWorkspace:
		return "w:" + r.workspace.ID
	}
	return ""
}

func (m sidebar) cursorKey() string {
	if r, ok := m.current(); ok {
		return rowKey(r)
	}
	return ""
}

func (m sidebar) current() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return row{}, false
	}
	return m.rows[m.visible[m.cursor]], true
}

func (m *sidebar) clamp() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
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
	h := m.height - 1
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
	case modeBranchPick:
		return m.viewBranchPick()
	case modeBranchManual:
		return m.viewBranchManual()
	case modeBranchMode:
		return m.viewBranchMode()
	case modeName:
		return m.viewName()
	}
	t := theme.Current
	var b strings.Builder

	if m.err != nil {
		b.WriteString(lipgloss.NewStyle().Foreground(t.Error).Render("error: "+m.err.Error()) + "\n")
	}

	if len(m.visible) == 0 && len(m.rows) > 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(0, 1).
			Render("no matches") + "\n")
	}
	if len(m.rows) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(t.TextMuted).Padding(1, 1).
			Render("no sessions\n\nutena new -branch <b> -repo <path> …") + "\n")
	}

	body := m.bodyHeight()
	drawn := 0
	for i := m.offset; i < len(m.visible) && i < m.offset+body; i++ {
		idx := m.visible[i]
		if idx < 0 || idx >= len(m.rows) {
			continue
		}
		b.WriteString(m.renderRow(m.rows[idx], i == m.cursor) + "\n")
		drawn++
	}
	for ; drawn < body; drawn++ {
		b.WriteString("\n")
	}

	foot := m.status
	if m.filtering {
		foot = m.input.View()
	} else if foot == "" {
		foot = m.help.View(m.keys)
	}
	b.WriteString(lipgloss.NewStyle().Foreground(t.Text).
		Padding(0, 1).Render(fitLine(foot, m.width)))
	return b.String()
}

func rowLine(r row, dirty map[string]int) string {
	t := theme.Current
	glyph, gc := statusGlyph(r.status)

	switch r.kind {
	case rowHeading:
		return lipgloss.NewStyle().Foreground(t.Tertiary).Render(r.heading)

	case rowWorkspace:
		return fmt.Sprintf("%s %s",
			lipgloss.NewStyle().Foreground(gc).Render(glyph),
			lipgloss.NewStyle().Foreground(t.Text).Render(r.workspace.Label))

	case rowSession:
		label := r.session.Name
		nameStyle := lipgloss.NewStyle().Foreground(t.Text)
		switch {
		case r.session.Broken:
			label = "! " + label
			nameStyle = lipgloss.NewStyle().Foreground(t.Error)
		case r.session.Archived:
			label = "⌁ " + label
			nameStyle = lipgloss.NewStyle().Foreground(t.TextMuted)
		case r.session.Active():
			nameStyle = lipgloss.NewStyle().Foreground(t.TextEmphasis).Bold(true)
		}
		chevron := " "
		if len(r.session.Checkouts) > 0 {
			chevron = "▸"
			if r.expanded {
				chevron = "▾"
			}
		}
		return fmt.Sprintf("%s %s %s",
			lipgloss.NewStyle().Foreground(t.TextMuted).Render(chevron),
			lipgloss.NewStyle().Foreground(gc).Render(glyph), nameStyle.Render(label))

	case rowCheckout:
		tree := "├─"
		if r.last {
			tree = "└─"
		}
		out := fmt.Sprintf("%s %s %s %s",
			lipgloss.NewStyle().Foreground(t.TextMuted).Render(tree),
			lipgloss.NewStyle().Foreground(gc).Render(glyph),
			lipgloss.NewStyle().Foreground(t.Text).Render(r.checkout.Label),
			lipgloss.NewStyle().Foreground(t.AccentBlue).Render(r.checkout.Branch))
		if n, ok := dirty[r.checkout.Path]; ok && n > 0 {
			out += lipgloss.NewStyle().Foreground(t.StatusPending).Render(fmt.Sprintf(" ●%d", n))
		}
		return out
	}
	return ""
}

func selectableRow(r row) bool {
	return r.kind == rowSession || r.kind == rowWorkspace
}

func rowTerm(r row) string {
	switch r.kind {
	case rowSession:
		parts := make([]string, 0, 1+2*len(r.session.Checkouts))
		parts = append(parts, r.session.Name)
		for _, c := range r.session.Checkouts {
			parts = append(parts, c.Label, c.Branch)
		}
		return strings.Join(parts, " ")
	case rowWorkspace:
		return r.workspace.Label
	}
	return ""
}

func ansiStrip(s string) string { return ansi.Strip(s) }

func fitLine(s string, width int) string {
	inner := width - 2
	if inner < 1 {
		inner = 1
	}
	return ansi.Truncate(s, inner, "…")
}

func (m sidebar) renderRow(r row, selected bool) string {
	line := fitLine(rowLine(r, m.dirty), m.width)
	style := lipgloss.NewStyle().Width(m.width).MaxWidth(m.width).Padding(0, 1)
	if selected {
		// Inner segments end in resets, which clear an outer background part-way
		// through the line. Strip them so the highlight covers the whole row.
		line = ansiStrip(line)
		style = style.
			Background(theme.Current.SurfaceActive).
			Foreground(theme.Current.TextEmphasis).
			Bold(true)
	}
	return style.Render(line)
}
