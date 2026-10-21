package ui

import (
	"math"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"

	"github.com/charmbracelet/lipgloss"
	"github.com/eleonorayaya/utena/internal/tui/theme"
)

type Field struct {
	Label string
	Value string
	Hint  string
	Error string
}

type Form struct {
	Title string
	count int
	focus int
}

func NewForm(title string, fields int) Form {
	return Form{Title: title, count: fields}
}

func (f Form) Focused() int {
	return f.focus
}

func (f Form) Focus(i int) Form {
	f.focus = min(max(i, 0), max(f.count-1, 0))
	return f
}

func (f Form) Next() Form {
	return f.Focus(f.focus + 1)
}

func (f Form) Prev() Form {
	return f.Focus(f.focus - 1)
}

func InputWidth(formWidth int, labels ...string) int {
	w := 0
	for _, l := range labels {
		w = max(w, lipgloss.Width(l))
	}
	return max(formWidth-GutterWidth-w-3, 10)
}

func NewInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = placeholder
	ti.Width = 40
	return ti
}

func FormTitle(title string) string {
	return lipgloss.NewStyle().Foreground(theme.Current.Primary).Bold(true).Render(title)
}

func LabelWidth(fields []Field) int {
	w := 0
	for _, fl := range fields {
		w = max(w, lipgloss.Width(fl.Label))
	}
	return w + 2
}

func (f Form) View(fields []Field, width int) string {
	if width <= 0 {
		width = math.MaxInt32
	}
	labelW := LabelWidth(fields)
	indent := strings.Repeat(" ", GutterWidth+labelW)
	labelStyle := fg(theme.Current.TextMuted)

	lines := []string{clampWidth(FormTitle(f.Title), width), ""}
	for i, fl := range fields {
		gutter := Gutter(i == f.focus)
		label := labelStyle.Render(fl.Label) + strings.Repeat(" ", labelW-lipgloss.Width(fl.Label))
		for j, v := range strings.Split(fl.Value, "\n") {
			prefix := indent
			if j == 0 {
				prefix = gutter + label
			}
			lines = append(lines, clampWidth(prefix+v, width))
		}
		if fl.Error != "" {
			lines = append(lines, clampWidth(indent+fg(theme.Current.Error).Render(Truncate(fl.Error, max(width-len(indent), 1))), width))
		} else if fl.Hint != "" && i == f.focus {
			lines = append(lines, clampWidth(indent+fg(theme.Current.TextMuted).Render(fl.Hint), width))
		}
	}
	return strings.Join(lines, "\n")
}
