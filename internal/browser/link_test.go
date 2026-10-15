package browser

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
)

type chromeWin struct {
	id    string
	title string
}

func TestLinkerResolve(t *testing.T) {
	tests := []struct {
		name      string
		cache     map[string]string
		chrome    []chromeWin
		wm        []wm.Window
		want      []string
		expected  map[string]string
		wantCache map[string]string
	}{
		{
			name:      "cache hit",
			cache:     map[string]string{"c1": "w1"},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: "Z"}, {ID: "w2", Title: "Y"}},
			want:      []string{"c1"},
			expected:  map[string]string{"c1": "w1"},
			wantCache: map[string]string{"c1": "w1"},
		},
		{
			name:      "stale wm entry dropped",
			cache:     map[string]string{"c1": "w9"},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: "X"}, {ID: "w2", Title: "Y"}},
			want:      []string{"c1"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "stale chrome entry dropped frees its wm window",
			cache:     map[string]string{"c9": "w1"},
			chrome:    []chromeWin{{"c1", "A"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}},
			want:      []string{"c1"},
			expected:  map[string]string{"c1": "w1"},
			wantCache: map[string]string{"c1": "w1"},
		},
		{
			name:      "last unlinked window is never guessed by elimination",
			cache:     map[string]string{"c1": "w1"},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "unrelated"}},
			want:      []string{"c2"},
			expected:  map[string]string{},
			wantCache: map[string]string{"c1": "w1"},
		},
		{
			name:      "no elimination while another chrome window is unlinked",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: "unrelated"}},
			want:      []string{"c2"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "unique title",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}, {"c3", "C"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "B"}, {ID: "w3", Title: "C"}},
			want:      []string{"c1", "c2"},
			expected:  map[string]string{"c1": "w1", "c2": "w2"},
			wantCache: map[string]string{"c1": "w1", "c2": "w2"},
		},
		{
			name:      "duplicate wm title gives no link",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "A"}},
			want:      []string{"c1"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "duplicate wanted chrome title gives no link",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "A"}, {"c3", "C"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "B"}},
			want:      []string{"c1", "c2"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "title shared with an unwanted unlinked chrome window gives no link",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "A"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "stale"}},
			want:      []string{"c1"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "title shared with a linked chrome window still links",
			cache:     map[string]string{"c1": "w1"},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "A"}},
			wm:        []wm.Window{{ID: "w1", Title: "stale"}, {ID: "w2", Title: "A"}},
			want:      []string{"c2"},
			expected:  map[string]string{"c2": "w2"},
			wantCache: map[string]string{"c1": "w1", "c2": "w2"},
		},
		{
			name:      "empty title gives no link",
			cache:     map[string]string{},
			chrome:    []chromeWin{{"c1", ""}, {"c2", "B"}},
			wm:        []wm.Window{{ID: "w1", Title: ""}, {ID: "w2", Title: "B"}},
			want:      []string{"c1"},
			expected:  map[string]string{},
			wantCache: map[string]string{},
		},
		{
			name:      "title already claimed by another link",
			cache:     map[string]string{"c1": "w1"},
			chrome:    []chromeWin{{"c1", "A"}, {"c2", "A"}, {"c3", "C"}},
			wm:        []wm.Window{{ID: "w1", Title: "A"}, {ID: "w2", Title: "B"}, {ID: "w3", Title: "D"}},
			want:      []string{"c2"},
			expected:  map[string]string{},
			wantCache: map[string]string{"c1": "w1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			titles := map[string]string{}
			chrome := make([]Window, len(tt.chrome))
			for i, c := range tt.chrome {
				chrome[i] = Window{ID: c.id}
				titles[c.id] = c.title
			}
			l := &linker{links: tt.cache}
			got := l.resolve(chrome, func(w Window) string { return titles[w.ID] }, tt.wm, tt.want)
			assert.Equal(t, tt.expected, got)
			assert.Equal(t, tt.wantCache, l.links)
		})
	}
}

func TestLinkerResolveNilCache(t *testing.T) {
	l := &linker{}
	got := l.resolve([]Window{{ID: "c1"}}, func(Window) string { return "" }, []wm.Window{{ID: "w1"}}, []string{"c1"})
	assert.Empty(t, got)
	assert.Empty(t, l.links)
}

func TestLinkerLinkCreated(t *testing.T) {
	before := []wm.Window{{ID: "w1"}}
	tests := []struct {
		name      string
		cache     map[string]string
		after     []wm.Window
		ok        bool
		wantCache map[string]string
	}{
		{"one new window", map[string]string{}, []wm.Window{{ID: "w1"}, {ID: "w2"}}, true, map[string]string{"c1": "w2"}},
		{"zero new windows", map[string]string{}, []wm.Window{{ID: "w1"}}, false, map[string]string{}},
		{"two new windows", map[string]string{}, []wm.Window{{ID: "w1"}, {ID: "w2"}, {ID: "w3"}}, false, map[string]string{}},
		{"only new window already claimed", map[string]string{"c0": "w2"}, []wm.Window{{ID: "w1"}, {ID: "w2"}}, false, map[string]string{"c0": "w2"}},
		{"claimed new window is ignored", map[string]string{"c0": "w2"}, []wm.Window{{ID: "w1"}, {ID: "w2"}, {ID: "w3"}}, true, map[string]string{"c0": "w2", "c1": "w3"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &linker{links: tt.cache}
			assert.Equal(t, tt.ok, l.linkCreated("c1", before, tt.after))
			assert.Equal(t, tt.wantCache, l.links)
		})
	}
}
