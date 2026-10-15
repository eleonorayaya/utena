package browser

import (
	"net/url"
	"strings"
)

func MatchTab(tabs []Tab, rawURL string) (Tab, bool) {
	want, ok := pathKey(rawURL)
	if !ok || len(want.segments) == 0 {
		return Tab{}, false
	}
	var best Tab
	found := false
	for _, t := range tabs {
		have, ok := pathKey(t.URL)
		if !ok || have.host != want.host || !hasSegmentPrefix(have.segments, want.segments) {
			continue
		}
		if len(have.segments) == len(want.segments) {
			return t, true
		}
		if !found {
			best, found = t, true
		}
	}
	return best, found
}

type urlKey struct {
	host     string
	segments []string
}

func pathKey(raw string) (urlKey, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return urlKey{}, false
	}
	var segs []string
	for _, s := range strings.Split(u.Path, "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return urlKey{host: strings.ToLower(u.Host), segments: segs}, true
}

func hasSegmentPrefix(have, want []string) bool {
	if len(have) < len(want) {
		return false
	}
	for i := range want {
		if have[i] != want[i] {
			return false
		}
	}
	return true
}
