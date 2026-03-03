package list

import (
	bubblelist "github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
)

func New(title string) bubblelist.Model {
	d := bubblelist.NewDefaultDelegate()
	d.Styles.NormalTitle = d.Styles.NormalTitle.Foreground(lipgloss.Color("252"))
	d.Styles.NormalDesc = d.Styles.NormalDesc.Foreground(lipgloss.Color("246"))
	l := bubblelist.New(nil, d, 0, 0)
	l.Title = title
	l.KeyMap.Quit.SetEnabled(false)
	l.SetShowHelp(false)
	return l
}
