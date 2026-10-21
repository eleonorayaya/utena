package ui

func ScrollOffset(offset, top, line, total, visible int) int {
	if top < offset {
		offset = top
	}
	if line >= offset+visible {
		offset = line - visible + 1
	}
	return max(min(offset, total-visible), 0)
}
