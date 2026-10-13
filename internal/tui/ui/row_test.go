package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func plain(text string, width int) Cell {
	return Cell{Spans: []Span{{Text: text}}, Width: width}
}

func TestRow_ExactWidth(t *testing.T) {
	out := Row([]Cell{plain("ab", 4), plain("name", 0), {Spans: []Span{{Text: "5m"}}, Width: 4, Align: AlignRight}}, 20, nil)
	assert.Equal(t, 20, lipgloss.Width(out))
	assert.Equal(t, "ab  name          5m", out)
}

func TestRow_TruncatesFlexCell(t *testing.T) {
	out := Row([]Cell{plain("x", 2), plain("a-very-long-session-name", 0), plain("1h", 3)}, 12, nil)
	assert.Equal(t, 12, lipgloss.Width(out))
	assert.Contains(t, out, "…")
}

func TestRow_MultipleSpansInOneCell(t *testing.T) {
	out := Row([]Cell{{Spans: []Span{{Text: "●1"}, {Text: " "}, {Text: "◌2"}}, Width: 7}}, 7, nil)
	assert.Equal(t, "●1 ◌2  ", out)
}

func TestRow_SpansTruncateAtCellWidth(t *testing.T) {
	out := Row([]Cell{{Spans: []Span{{Text: "abc"}, {Text: "defgh"}}, Width: 5}}, 5, nil)
	assert.Equal(t, 5, lipgloss.Width(out))
}

func TestRow_FixedCellsWiderThanRowDoNotPanic(t *testing.T) {
	out := Row([]Cell{plain("abcdef", 6), plain("name", 0), plain("xyz", 3)}, 5, nil)
	assert.LessOrEqual(t, lipgloss.Width(out), 5)
}

func TestRow_BackgroundDoesNotChangeWidth(t *testing.T) {
	out := Row([]Cell{plain("a", 2), plain("b", 0)}, 10, lipgloss.Color("#ff0000"))
	assert.Equal(t, 10, lipgloss.Width(out))
}
