package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Span struct {
	Text  string
	Style lipgloss.Style
}

func (s Span) Render() string {
	return s.Style.Render(s.Text)
}

type Align int

const (
	AlignLeft Align = iota
	AlignRight
)

type Cell struct {
	Spans []Span
	Width int
	Align Align
}

func Row(cells []Cell, width int, bg lipgloss.TerminalColor) string {
	widths := cellWidths(cells, width)
	var b strings.Builder
	for i, c := range cells {
		b.WriteString(renderCell(c, widths[i], bg))
	}
	return clampWidth(b.String(), width)
}

func cellWidths(cells []Cell, width int) []int {
	widths := make([]int, len(cells))
	fixed, flex := 0, 0
	for i, c := range cells {
		if c.Width > 0 {
			widths[i] = c.Width
			fixed += c.Width
		} else {
			flex++
		}
	}
	if flex == 0 {
		return widths
	}
	remaining := max(width-fixed, 0)
	share, extra := remaining/flex, remaining%flex
	for i, c := range cells {
		if c.Width > 0 {
			continue
		}
		widths[i] = share + extra
		extra = 0
	}
	return widths
}

func renderCell(c Cell, width int, bg lipgloss.TerminalColor) string {
	if width <= 0 {
		return ""
	}
	withBg := func(s lipgloss.Style) lipgloss.Style {
		if bg == nil {
			return s
		}
		return s.Background(bg)
	}

	var parts []string
	used := 0
	for _, sp := range c.Spans {
		if used >= width {
			break
		}
		text := sp.Text
		if lipgloss.Width(text) > width-used {
			text = Truncate(text, width-used)
		}
		used += lipgloss.Width(text)
		parts = append(parts, withBg(sp.Style).Render(text))
	}

	pad := withBg(lipgloss.NewStyle()).Render(strings.Repeat(" ", max(width-used, 0)))
	if c.Align == AlignRight {
		return pad + strings.Join(parts, "")
	}
	return strings.Join(parts, "") + pad
}
