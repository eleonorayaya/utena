package sessionlist

import (
	"sort"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
)

type sectionKind int

const (
	sectionNeedsYou sectionKind = iota
	sectionWorking
	sectionIdle
	sectionBlocked
	sectionHidden
)

func (k sectionKind) title() string {
	switch k {
	case sectionNeedsYou:
		return "Needs you"
	case sectionWorking:
		return "Working"
	case sectionIdle:
		return "Idle"
	case sectionBlocked:
		return "Blocked"
	default:
		return "Hidden"
	}
}

func sectionOf(s session.Session) sectionKind {
	switch {
	case s.IsHidden():
		return sectionHidden
	case s.IsBlocked():
		return sectionBlocked
	}
	switch s.AttentionStatus() {
	case claude.StatusNeedsAttention, claude.StatusReadyForReview:
		return sectionNeedsYou
	case claude.StatusWorking:
		return sectionWorking
	default:
		return sectionIdle
	}
}

func sortSessions(ss []session.Session) {
	sort.SliceStable(ss, func(i, j int) bool {
		si, sj := sectionOf(ss[i]), sectionOf(ss[j])
		if si != sj {
			return si < sj
		}
		return ss[i].LastUsedAt.After(ss[j].LastUsedAt)
	})
}

type rowKind int

const (
	rowSession rowKind = iota
	rowHeader
	rowGap
)

type listRow struct {
	kind    rowKind
	section sectionKind
	count   int
	index   int
}

func layoutRows(ss []session.Session) []listRow {
	counts := map[sectionKind]int{}
	for _, s := range ss {
		counts[sectionOf(s)]++
	}
	var rows []listRow
	for i, s := range ss {
		sec := sectionOf(s)
		if i == 0 || sectionOf(ss[i-1]) != sec {
			if i > 0 {
				rows = append(rows, listRow{kind: rowGap})
			}
			rows = append(rows, listRow{kind: rowHeader, section: sec, count: counts[sec]})
		}
		rows = append(rows, listRow{kind: rowSession, section: sec, index: i})
	}
	return rows
}

func rowOfSession(rows []listRow, index int) int {
	for i, r := range rows {
		if r.kind == rowSession && r.index == index {
			return i
		}
	}
	return -1
}
