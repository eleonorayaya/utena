package ui

import (
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func NewList() list.Model {
	l := list.New(nil, ListDelegate{}, 0, 0)
	StyleList(&l)
	return l
}

func StyleList(l *list.Model) {
	l.SetShowTitle(false)
	l.SetShowFilter(false)
	l.SetShowStatusBar(false)
	l.SetShowHelp(false)
	l.SetShowPagination(false)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.FilterInput.Prompt = "/ "
	l.FilterInput.PromptStyle = fg(theme.Current.TextMuted)
	l.FilterInput.TextStyle = fg(theme.Current.Text)
	l.FilterInput.Cursor.Style = fg(theme.Current.Primary)
}

func FilterActive(l list.Model) bool {
	return l.FilterState() != list.Unfiltered
}

func FilterTyping(l list.Model) bool {
	return l.FilterState() == list.Filtering
}

func ListView(l list.Model) string {
	switch l.FilterState() {
	case list.Filtering:
		line := l.FilterInput.View()
		l.SetHeight(max(l.Height()-1, 1))
		return clampWidth(line, l.Width()) + "\n" + l.View()
	case list.FilterApplied:
		line := fg(theme.Current.TextMuted).Render("/ " + l.FilterValue())
		l.SetHeight(max(l.Height()-1, 1))
		return clampWidth(line, l.Width()) + "\n" + l.View()
	}
	return l.View()
}

func UpdateList(l list.Model, msg tea.Msg) (list.Model, tea.Cmd) {
	prev := l.Index()
	l, cmd := l.Update(msg)
	if _, ok := msg.(list.FilterMatchesMsg); ok {
		l.Select(0)
		prev = 0
	}
	SkipUnselectable(&l, prev)
	return l, cmd
}

func SetItems(l *list.Model, items []list.Item) {
	cmd := l.SetItems(items)
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(list.FilterMatchesMsg); ok {
		*l, _ = l.Update(msg)
	}
}

func SelectWhere(l *list.Model, match func(list.Item) bool) bool {
	for i, it := range l.VisibleItems() {
		if match(it) {
			l.Select(i)
			return true
		}
	}
	idx := min(l.Index(), max(len(l.VisibleItems())-1, 0))
	l.Select(idx)
	SkipUnselectable(l, idx)
	return false
}

func SkipUnselectable(l *list.Model, prev int) {
	items := l.VisibleItems()
	idx := l.Index()
	if idx < 0 || idx >= len(items) || Selectable(items[idx]) {
		return
	}
	dir := 1
	if idx < prev {
		dir = -1
	}
	for _, d := range []int{dir, -dir} {
		for i := idx + d; i >= 0 && i < len(items); i += d {
			if Selectable(items[i]) {
				l.Select(i)
				return
			}
		}
	}
}
