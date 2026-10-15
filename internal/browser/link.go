package browser

import "github.com/eleonorayaya/utena/internal/wm"

type linker struct {
	links map[string]string
}

func (l *linker) resolve(chrome []Window, title func(Window) string, wmWindows []wm.Window, want []string) map[string]string {
	if l.links == nil {
		l.links = map[string]string{}
	}

	wmIDs := make(map[string]bool, len(wmWindows))
	for _, w := range wmWindows {
		wmIDs[w.ID] = true
	}
	chromeByID := make(map[string]Window, len(chrome))
	for _, w := range chrome {
		chromeByID[w.ID] = w
	}
	for chromeID, wmID := range l.links {
		if _, ok := chromeByID[chromeID]; !ok || !wmIDs[wmID] {
			delete(l.links, chromeID)
		}
	}

	result := map[string]string{}
	var remaining []string
	seen := map[string]bool{}
	for _, id := range want {
		if seen[id] {
			continue
		}
		seen[id] = true
		if wmID, ok := l.links[id]; ok {
			result[id] = wmID
			continue
		}
		if _, ok := chromeByID[id]; ok {
			remaining = append(remaining, id)
		}
	}

	claimed := make(map[string]bool, len(l.links))
	for _, wmID := range l.links {
		claimed[wmID] = true
	}
	var candidates []wm.Window
	for _, w := range wmWindows {
		if !claimed[w.ID] {
			candidates = append(candidates, w)
		}
	}

	link := func(chromeID, wmID string) {
		l.links[chromeID] = wmID
		result[chromeID] = wmID
	}

	titleCounts := map[string]int{}
	for _, w := range chrome {
		if _, ok := l.links[w.ID]; !ok {
			titleCounts[title(w)]++
		}
	}
	for _, id := range remaining {
		t := title(chromeByID[id])
		if t == "" || titleCounts[t] != 1 {
			continue
		}
		var match string
		matches := 0
		for _, c := range candidates {
			if c.Title == t {
				match = c.ID
				matches++
			}
		}
		if matches == 1 {
			link(id, match)
		}
	}

	return result
}

func (l *linker) linkCreated(chromeID string, before, after []wm.Window) bool {
	existing := make(map[string]bool, len(before)+len(l.links))
	for _, w := range before {
		existing[w.ID] = true
	}
	for _, wmID := range l.links {
		existing[wmID] = true
	}
	var created []string
	for _, w := range after {
		if !existing[w.ID] {
			created = append(created, w.ID)
		}
	}
	if len(created) != 1 {
		return false
	}
	if l.links == nil {
		l.links = map[string]string{}
	}
	l.links[chromeID] = created[0]
	return true
}
