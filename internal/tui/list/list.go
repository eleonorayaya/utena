package list

import (
	bubblelist "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

func applySharedStyles(l *bubblelist.Model) {
	l.Styles.TitleBar = l.Styles.TitleBar.
		Background(lipgloss.NoColor{}).
		PaddingLeft(0).
		PaddingTop(0).
		PaddingBottom(1)
	l.Styles.Title = l.Styles.Title.
		Foreground(theme.Current.Primary).
		Background(lipgloss.NoColor{}).
		Bold(true)
	l.SetShowStatusBar(false)
}

func New(title string) bubblelist.Model {
	d := bubblelist.NewDefaultDelegate()
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(theme.Current.ListTitle)
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(theme.Current.ListDesc)
	d.Styles.SelectedTitle = d.Styles.SelectedTitle.
		Foreground(theme.Current.ListSelectedTitle).
		BorderForeground(theme.Current.ListSelectedBorder)
	d.Styles.SelectedDesc = d.Styles.SelectedDesc.
		Foreground(theme.Current.ListSelectedDesc).
		BorderForeground(theme.Current.ListSelectedBorder)
	l := bubblelist.New(nil, d, 0, 0)
	l.Title = title
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	applySharedStyles(&l)
	return l
}

func NewWithDelegate(title string, d bubblelist.ItemDelegate) bubblelist.Model {
	l := bubblelist.New(nil, d, 0, 0)
	l.Title = title
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	applySharedStyles(&l)
	return l
}
