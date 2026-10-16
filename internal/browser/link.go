package browser

import "github.com/eleonorayaya/utena/internal/wm"

type linker struct {
	links map[string]string
}

func (l *linker) resolve(windows []Window, title func(Window) string, wmWindows []wm.Window, want []string) map[string]string {
	if l.links == nil {
		l.links = map[string]string{}
	}

	wmIDs := make(map[string]bool, len(wmWindows))
	for _, w := range wmWindows {
		wmIDs[w.ID] = true
	}
	byID := make(map[string]Window, len(windows))
	for _, w := range windows {
		byID[w.ID] = w
	}
	for windowID, wmID := range l.links {
		if _, ok := byID[windowID]; !ok || !wmIDs[wmID] {
			delete(l.links, windowID)
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
		if _, ok := byID[id]; ok {
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

	link := func(windowID, wmID string) {
		l.links[windowID] = wmID
		result[windowID] = wmID
	}

	titleCounts := map[string]int{}
	for _, w := range windows {
		if _, ok := l.links[w.ID]; !ok {
			titleCounts[title(w)]++
		}
	}
	for _, id := range remaining {
		t := title(byID[id])
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

func (l *linker) linkCreated(windowID string, before, after []wm.Window) bool {
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
	l.links[windowID] = created[0]
	return true
}
