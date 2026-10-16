package browser

import (
	"net/url"
	"sort"
	"strings"
)

const reattachThreshold = 0.5

type restoreCandidate struct {
	windowID string
	rowID    uint
	score    float64
}

func normalizeURL(raw string) string {
	if !isWebURL(raw) {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host) + strings.TrimSuffix(u.Path, "/")
}

func urlSet(urls []string) map[string]bool {
	set := make(map[string]bool, len(urls))
	for _, raw := range urls {
		if key := normalizeURL(raw); key != "" {
			set[key] = true
		}
	}
	return set
}

func windowURLs(w Window) []string {
	urls := make([]string, 0, len(w.Tabs))
	for _, t := range w.Tabs {
		urls = append(urls, t.URL)
	}
	return urls
}

func savedURLs(tabs []SavedTab) []string {
	urls := make([]string, 0, len(tabs))
	for _, t := range tabs {
		urls = append(urls, t.URL)
	}
	return urls
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for k := range a {
		if b[k] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

func matchRestored(rows []BrowserWindow, windows []Window, owned map[string]bool, threshold float64) map[string]uint {
	windowSets := map[string]map[string]bool{}
	for _, w := range windows {
		if owned[w.ID] {
			continue
		}
		if set := urlSet(windowURLs(w)); len(set) > 0 {
			windowSets[w.ID] = set
		}
	}

	var candidates []restoreCandidate
	for _, r := range rows {
		if len(r.Tabs) == 0 {
			continue
		}
		rowSet := urlSet(savedURLs(r.Tabs))
		for id, set := range windowSets {
			if score := jaccard(rowSet, set); score >= threshold {
				candidates = append(candidates, restoreCandidate{windowID: id, rowID: r.ID, score: score})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.score != b.score {
			return a.score > b.score
		}
		if a.rowID != b.rowID {
			return a.rowID < b.rowID
		}
		return a.windowID < b.windowID
	})

	usedWindows := map[string]bool{}
	usedRows := map[uint]bool{}
	matches := map[string]uint{}
	for _, c := range candidates {
		if usedWindows[c.windowID] || usedRows[c.rowID] {
			continue
		}
		var rivals []restoreCandidate
		for _, o := range candidates {
			if o == c || o.score != c.score || usedWindows[o.windowID] || usedRows[o.rowID] {
				continue
			}
			if o.windowID == c.windowID || o.rowID == c.rowID {
				rivals = append(rivals, o)
			}
		}
		usedWindows[c.windowID] = true
		usedRows[c.rowID] = true
		if len(rivals) > 0 {
			for _, o := range rivals {
				usedWindows[o.windowID] = true
				usedRows[o.rowID] = true
			}
			continue
		}
		matches[c.windowID] = c.rowID
	}
	return matches
}
