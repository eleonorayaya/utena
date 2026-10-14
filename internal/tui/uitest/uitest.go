package uitest

import (
	"reflect"

	tea "github.com/charmbracelet/bubbletea"
)

func Drain(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, Drain(c)...)
		}
		return out
	default:
		return []tea.Msg{m}
	}
}

func MsgTypes(cmd tea.Cmd) []string {
	var out []string
	for _, m := range Drain(cmd) {
		out = append(out, reflect.TypeOf(m).String())
	}
	return out
}

func Runes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}
