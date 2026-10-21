package sessiondetail

import (
	"fmt"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/tui/addworkspaceform"
	"github.com/eleonorayaya/utena/internal/tui/blockerform"
	"github.com/eleonorayaya/utena/internal/tui/bundlepicker"
	"github.com/eleonorayaya/utena/internal/tui/provider"
	"github.com/eleonorayaya/utena/internal/tui/router"
	"github.com/eleonorayaya/utena/internal/tui/sessionprogress"
	"github.com/eleonorayaya/utena/internal/tui/ui"
)

type Model struct {
	sess                   *session.Session
	pendingDeleteID        uint
	focused                bool
	cursor                 int
	offset                 int
	browserState           browser.SessionsView
	pendingBlockerDeleteID uint
	width                  int
	height                 int
	statusMsg              string
	statusKind             ui.StatusKind
	bundlePicker           bundlepicker.Model
	pickingBundles         bool
}

func New() Model {
	return Model{bundlePicker: bundlepicker.New("Plugin bundles"), cursor: -1}
}

func (m Model) Init() tea.Cmd {
	if m.bundlePicker.Catalog() == nil {
		return provider.FetchBundles()
	}
	return nil
}

func (m Model) CapturesText() bool {
	return m.pickingBundles && m.bundlePicker.CapturesText()
}

func (m Model) Focused() bool {
	return m.focused
}

func (m Model) SessionID() uint {
	if m.sess == nil {
		return 0
	}
	return m.sess.ID
}

func (m *Model) setStatus(text string, kind ui.StatusKind) {
	m.statusMsg = text
	m.statusKind = kind
}

func (m Model) Status() ui.Status {
	if m.pickingBundles {
		return ui.Status{}
	}
	return ui.Status{Text: m.statusMsg, Kind: m.statusKind}
}

func (m Model) Select(s *session.Session) Model {
	if s == nil {
		m.sess = nil
		m.reset()
		return m
	}
	fresh := *s
	if m.sess == nil || m.sess.ID != fresh.ID {
		m.sess = &fresh
		m.reset()
		return m
	}
	sel := m.currentSelection()
	m.sess = &fresh
	if fresh.Status == session.StatusDeleted {
		m.pickingBundles = false
	}
	m.restoreSelection(sel)
	m.ensureVisible()
	return m
}

func (m *Model) reset() {
	m.clearTransient()
	m.focused = false
	m.cursor = 0
	m.clampCursor()
}

func (m *Model) clearTransient() {
	m.pendingDeleteID = 0
	m.pendingBlockerDeleteID = 0
	m.pickingBundles = false
	m.offset = 0
	m.setStatus("", ui.StatusInfo)
}

func (m Model) Focus() Model {
	m.focused = true
	m.setStatus("", ui.StatusInfo)
	m.clampCursor()
	m.ensureVisible()
	return m
}

func (m Model) FocusPR() Model {
	m.focused = true
	m.setStatus("", ui.StatusInfo)
	m.clampCursor()
	openable := map[string]bool{}
	if m.sess != nil {
		for _, pr := range ui.OpenablePRs(m.sess.PullRequests) {
			openable[pr.HTMLURL] = true
		}
	}
	for i, it := range m.items() {
		if it.kind == prItem && openable[it.prURL] {
			m.cursor = i
			break
		}
	}
	m.ensureVisible()
	return m
}

func (m Model) Blur() Model {
	m.focused = false
	m.clearTransient()
	return m
}

type tabRef struct {
	windowID string
	tabID    string
}

func focusable(w browser.WindowView, t browser.TabView) bool {
	return w.ID != "" && t.ID != ""
}

func (m Model) sessionBrowser() browser.SessionBrowser {
	if m.sess == nil {
		return browser.SessionBrowser{}
	}
	return m.browserState.Sessions[m.sess.ID]
}

func (m Model) selectableTabs() []tabRef {
	var out []tabRef
	for _, w := range m.sessionBrowser().Windows {
		for _, t := range w.Tabs {
			if focusable(w, t) {
				out = append(out, tabRef{windowID: w.ID, tabID: t.ID})
			}
		}
	}
	return out
}

func (m Model) selectablePRs() [][]git.PullRequest {
	var out [][]git.PullRequest
	for _, g := range groupPRs(*m.sess) {
		var prs []git.PullRequest
		for _, pr := range g.prs {
			if pr.HTMLURL != "" {
				prs = append(prs, pr)
			}
		}
		out = append(out, prs)
	}
	return out
}

type itemKind int

const (
	blockerItem itemKind = iota
	tabItem
	prItem
)

type itemKey struct {
	kind      itemKind
	blockerID uint
	tab       tabRef
	prURL     string
}

type item struct {
	itemKey
	section int
}

func (m Model) items() []item {
	if m.sess == nil {
		return nil
	}
	var out []item
	for _, b := range m.sess.OpenBlockers() {
		out = append(out, item{itemKey: itemKey{kind: blockerItem, blockerID: b.ID}})
	}
	for _, t := range m.selectableTabs() {
		out = append(out, item{itemKey: itemKey{kind: tabItem, tab: t}})
	}
	for g, prs := range m.selectablePRs() {
		for _, pr := range prs {
			out = append(out, item{itemKey: itemKey{kind: prItem, prURL: pr.HTMLURL}, section: g})
		}
	}
	return out
}

type selection struct {
	item      item
	ok        bool
	kindIndex int
}

func (m Model) currentSelection() selection {
	items := m.items()
	if m.cursor < 0 || m.cursor >= len(items) {
		return selection{}
	}
	sel := selection{item: items[m.cursor], ok: true}
	for _, it := range items[:m.cursor] {
		if it.kind == sel.item.kind {
			sel.kindIndex++
		}
	}
	return sel
}

func (m *Model) restoreSelection(sel selection) {
	if !sel.ok {
		m.clampCursor()
		return
	}
	items := m.items()
	var sameKind []int
	for i, it := range items {
		if it.itemKey == sel.item.itemKey {
			m.cursor = i
			return
		}
		if it.kind == sel.item.kind {
			sameKind = append(sameKind, i)
		}
	}
	if len(sameKind) > 0 {
		m.cursor = sameKind[min(sel.kindIndex, len(sameKind)-1)]
		return
	}
	m.clampCursor()
}

func (m *Model) clampCursor() {
	n := len(m.items())
	if n == 0 {
		m.cursor = -1
		return
	}
	m.cursor = max(min(m.cursor, n-1), 0)
}

func (m *Model) jumpSection(dir int) {
	items := m.items()
	if m.cursor < 0 || m.cursor >= len(items) {
		return
	}
	var starts []int
	for i, it := range items {
		if i == 0 || it.kind != items[i-1].kind || it.section != items[i-1].section {
			starts = append(starts, i)
		}
	}
	if dir > 0 {
		for _, s := range starts {
			if s > m.cursor {
				m.cursor = s
				return
			}
		}
		return
	}
	for i := len(starts) - 1; i >= 0; i-- {
		if starts[i] < m.cursor {
			m.cursor = starts[i]
			return
		}
	}
}

func (m Model) selectedItem() *item {
	items := m.items()
	if m.cursor < 0 || m.cursor >= len(items) {
		return nil
	}
	return &items[m.cursor]
}

func (m Model) selectedBlocker() *session.SessionBlocker {
	it := m.selectedItem()
	if it == nil || it.kind != blockerItem {
		return nil
	}
	for _, b := range m.sess.OpenBlockers() {
		if b.ID == it.blockerID {
			return &b
		}
	}
	return nil
}

func (m Model) selectedTab() *tabRef {
	it := m.selectedItem()
	if it == nil || it.kind != tabItem {
		return nil
	}
	return &it.tab
}

func (m Model) selectedPRURL() string {
	it := m.selectedItem()
	if it == nil || it.kind != prItem {
		return ""
	}
	return it.prURL
}

func (m Model) Keys() help.KeyMap {
	if m.pickingBundles {
		return m.bundlePicker.Keys()
	}
	k := keys
	items := m.items()
	if m.cursor >= 0 && m.cursor < len(items) {
		switch items[m.cursor].kind {
		case tabItem:
			k.Activate = openTabKey
			k.Open = openTabKey
		case prItem:
			k.Open = openPRKey
			k.showOpen = true
		}
	}
	k.showNext = len(items) > 1
	return k
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.bundlePicker.SetSize(msg.Width, msg.Height)
		m.ensureVisible()
		return m, nil
	case provider.BundlesLoadedMsg:
		if msg.Err == nil {
			m.bundlePicker = m.bundlePicker.SetCatalog(msg.Bundles)
		}
		return m, nil
	case provider.SessionBundlesSetMsg:
		if m.sess != nil && msg.SessionID == m.sess.ID {
			m.setStatus("bundles saved · run /reload-plugins and /mcp in open Claude panes", ui.StatusInfo)
		}
		return m, nil
	case bundlepicker.SelectedMsg:
		m.pickingBundles = false
		if m.sess != nil {
			return m, provider.SetSessionBundles(m.sess.ID, msg.Bundles)
		}
		return m, nil
	case bundlepicker.ClosedMsg:
		m.pickingBundles = false
		return m, nil
	case provider.BrowserStateUpdatedMsg:
		if m.sess == nil {
			m.browserState = msg.State
			return m, nil
		}
		sel := m.currentSelection()
		m.browserState = msg.State
		m.restoreSelection(sel)
		m.ensureVisible()
		return m, nil
	case tea.KeyMsg:
		if !m.pickingBundles {
			var cmd tea.Cmd
			m, cmd = m.onKeyMsg(msg)
			m.ensureVisible()
			return m, cmd
		}
	}
	if m.pickingBundles {
		var cmd tea.Cmd
		m.bundlePicker, cmd = m.bundlePicker.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) onKeyMsg(msg tea.KeyMsg) (Model, tea.Cmd) {
	if !m.focused {
		return m, nil
	}
	if key.Matches(msg, keys.Back) {
		return m.Blur(), nil
	}
	if m.sess == nil {
		return m, nil
	}

	m.setStatus("", ui.StatusInfo)

	pendingID := m.pendingDeleteID
	m.pendingDeleteID = 0
	pendingBlockerID := m.pendingBlockerDeleteID
	m.pendingBlockerDeleteID = 0

	switch {
	case key.Matches(msg, keys.Up):
		m.cursor--
		m.clampCursor()
		return m, nil

	case key.Matches(msg, keys.Down):
		m.cursor++
		m.clampCursor()
		return m, nil

	case key.Matches(msg, keys.NextSection):
		m.jumpSection(1)
		return m, nil

	case key.Matches(msg, keys.PrevSection):
		m.jumpSection(-1)
		return m, nil

	case key.Matches(msg, keys.Block):
		if m.sess.Status == session.StatusDeleted {
			return m, nil
		}
		return m, tea.Sequence(
			router.NavigateTo(router.BlockerFormView),
			blockerform.Start(m.sess.ID),
		)

	case key.Matches(msg, keys.Resolve):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		return m, provider.ResolveBlocker(m.sess.ID, b.ID)

	case key.Matches(msg, keys.RemoveBlocker):
		b := m.selectedBlocker()
		if b == nil {
			return m, nil
		}
		if pendingBlockerID == b.ID {
			return m, provider.DeleteBlocker(m.sess.ID, b.ID)
		}
		m.pendingBlockerDeleteID = b.ID
		m.setStatus("press x again to delete this blocker", ui.StatusWarning)
		return m, nil

	case key.Matches(msg, keys.Activate):
		if t := m.selectedTab(); t != nil {
			return m, provider.FocusTab(m.sess.ID, t.windowID, t.tabID)
		}
		if m.sess.IsAttached {
			return m, nil
		}
		return m, provider.ActivateSession(m.sess.ID)

	case key.Matches(msg, keys.Open):
		if t := m.selectedTab(); t != nil {
			return m, provider.FocusTab(m.sess.ID, t.windowID, t.tabID)
		}
		if url := m.selectedPRURL(); url != "" {
			return m, provider.OpenURL(m.sess.ID, url)
		}
		m.setStatus("select a tab or pull request to open", ui.StatusInfo)
		return m, nil

	case key.Matches(msg, keys.Archive):
		if !m.sess.CanArchive() {
			return m, nil
		}
		id := m.sess.ID
		return m.Blur(), provider.ArchiveSession(id)

	case key.Matches(msg, keys.Delete):
		// Only archived sessions can be deleted; creating sessions keep the
		// force escape hatch.
		if !m.sess.CanDelete() && !m.sess.IsCreating() {
			return m, nil
		}
		if pendingID == m.sess.ID {
			cmd := provider.DeleteSession(m.sess.ID, m.sess.IsCreating())
			return m.Blur(), cmd
		}
		m.pendingDeleteID = m.sess.ID
		m.setStatus(fmt.Sprintf("press d again to delete %s (removes worktrees)", m.sess.DisplayLabel()), ui.StatusWarning)
		return m, nil

	case key.Matches(msg, keys.Repair):
		if m.sess.Status != session.StatusBroken {
			return m, nil
		}
		id := m.sess.ID
		return m, tea.Sequence(
			router.NavigateTo(router.SessionProgressView),
			sessionprogress.StartRepair(id),
		)

	case key.Matches(msg, keys.AddWorkspace):
		s := m.sess.Status
		if s == session.StatusDeleted || s == session.StatusArchived || s == session.StatusCreating {
			return m, nil
		}
		id := m.sess.ID
		return m, tea.Sequence(
			router.NavigateTo(router.AddWorkspaceFormView),
			addworkspaceform.Start(id),
		)

	case key.Matches(msg, keys.Bundles):
		if m.sess.Status == session.StatusDeleted {
			return m, nil
		}
		m.bundlePicker = m.bundlePicker.Open(m.sess.Bundles)
		m.pickingBundles = true
		return m, nil
	}

	return m, nil
}
