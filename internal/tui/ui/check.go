package ui

import (
	"slices"

	"github.com/eleonorayaya/utena/internal/tui/theme"
)

const checkColW = 4

func CheckCell(checked bool) Cell {
	box := Span{Text: "[ ]", Style: FG(theme.Current.TextMuted)}
	if checked {
		box = Span{Text: "[x]", Style: FG(theme.Current.Primary)}
	}
	return Cell{Spans: []Span{box}, Width: checkColW}
}

func ToggleSelected[T comparable](sel []T, v T) []T {
	if i := slices.Index(sel, v); i >= 0 {
		return slices.Delete(sel, i, i+1)
	}
	return append(sel, v)
}
