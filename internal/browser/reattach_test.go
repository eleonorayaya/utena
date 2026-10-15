package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func restorable(id uint, urls ...string) BrowserWindow {
	row := BrowserWindow{Model: gorm.Model{ID: id}, State: WindowMissing}
	for _, u := range urls {
		row.Tabs = append(row.Tabs, SavedTab{URL: u})
	}
	return row
}

func liveWindow(id string, urls ...string) Window {
	w := Window{ID: id}
	for _, u := range urls {
		w.Tabs = append(w.Tabs, Tab{URL: u})
	}
	return w
}

func TestMatchRestored(t *testing.T) {
	tests := []struct {
		name    string
		rows    []BrowserWindow
		windows []Window
		owned   map[string]bool
		want    map[string]uint
	}{
		{
			name:    "exact match",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com/x", "https://b.example.com")},
			windows: []Window{liveWindow("w1", "https://b.example.com", "https://a.example.com/x")},
			want:    map[string]uint{"w1": 1},
		},
		{
			name:    "normalizes query fragment trailing slash and host case",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com/x/?q=1#top", "https://B.example.com/")},
			windows: []Window{liveWindow("w1", "https://a.example.com/x", "https://b.example.com")},
			want:    map[string]uint{"w1": 1},
		},
		{
			name:    "partial match above threshold",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com", "https://b.example.com", "https://c.example.com")},
			windows: []Window{liveWindow("w1", "https://a.example.com", "https://b.example.com", "https://c.example.com", "https://d.example.com")},
			want:    map[string]uint{"w1": 1},
		},
		{
			name:    "score exactly at threshold matches",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com", "https://b.example.com")},
			windows: []Window{liveWindow("w1", "https://a.example.com")},
			want:    map[string]uint{"w1": 1},
		},
		{
			name:    "below threshold",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com", "https://b.example.com", "https://c.example.com")},
			windows: []Window{liveWindow("w1", "https://a.example.com", "https://d.example.com")},
			want:    map[string]uint{},
		},
		{
			name: "tie on the window side",
			rows: []BrowserWindow{
				restorable(1, "https://a.example.com", "https://b.example.com"),
				restorable(2, "https://a.example.com", "https://b.example.com"),
			},
			windows: []Window{liveWindow("w1", "https://a.example.com", "https://b.example.com")},
			want:    map[string]uint{},
		},
		{
			name: "tie on the row side",
			rows: []BrowserWindow{restorable(1, "https://a.example.com", "https://b.example.com")},
			windows: []Window{
				liveWindow("w1", "https://a.example.com", "https://b.example.com"),
				liveWindow("w2", "https://a.example.com", "https://b.example.com"),
			},
			want: map[string]uint{},
		},
		{
			name: "tie excludes every participant",
			rows: []BrowserWindow{
				restorable(1, "https://a.example.com", "https://b.example.com"),
				restorable(2, "https://a.example.com", "https://b.example.com", "https://c.example.com"),
			},
			windows: []Window{
				liveWindow("w1", "https://a.example.com", "https://b.example.com"),
				liveWindow("w2", "https://a.example.com", "https://b.example.com"),
			},
			want: map[string]uint{},
		},
		{
			name: "higher unique pair resolves a lower tie",
			rows: []BrowserWindow{
				restorable(1, "https://a.example.com", "https://b.example.com"),
				restorable(2, "https://a.example.com", "https://c.example.com"),
			},
			windows: []Window{
				liveWindow("w1", "https://a.example.com", "https://b.example.com"),
				liveWindow("w2", "https://a.example.com"),
			},
			want: map[string]uint{"w1": 1, "w2": 2},
		},
		{
			name: "best pairs win greedily",
			rows: []BrowserWindow{
				restorable(1, "https://a.example.com", "https://b.example.com", "https://c.example.com"),
				restorable(2, "https://x.example.com", "https://y.example.com"),
			},
			windows: []Window{
				liveWindow("w1", "https://x.example.com", "https://y.example.com", "https://z.example.com"),
				liveWindow("w2", "https://a.example.com", "https://b.example.com", "https://c.example.com"),
			},
			want: map[string]uint{"w1": 2, "w2": 1},
		},
		{
			name:    "owned windows excluded",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com")},
			windows: []Window{liveWindow("w1", "https://a.example.com")},
			owned:   map[string]bool{"w1": true},
			want:    map[string]uint{},
		},
		{
			name:    "new tab only windows do not match",
			rows:    []BrowserWindow{restorable(1, "chrome://newtab/")},
			windows: []Window{liveWindow("w1", "chrome://newtab/")},
			want:    map[string]uint{},
		},
		{
			name:    "new tabs are ignored when scoring",
			rows:    []BrowserWindow{restorable(1, "https://a.example.com")},
			windows: []Window{liveWindow("w1", "chrome://newtab/", "https://a.example.com")},
			want:    map[string]uint{"w1": 1},
		},
		{
			name:    "rows without tabs are skipped",
			rows:    []BrowserWindow{restorable(1)},
			windows: []Window{liveWindow("w1")},
			want:    map[string]uint{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, matchRestored(tt.rows, tt.windows, tt.owned, 0.5))
		})
	}
}

func TestMatchRestored_OrderIndependent(t *testing.T) {
	rows := []BrowserWindow{
		restorable(1, "https://a.example.com", "https://b.example.com"),
		restorable(2, "https://a.example.com", "https://b.example.com"),
		restorable(3, "https://c.example.com", "https://d.example.com"),
		restorable(4, "https://e.example.com", "https://f.example.com", "https://g.example.com"),
	}
	windows := []Window{
		liveWindow("w1", "https://a.example.com", "https://b.example.com"),
		liveWindow("w2", "https://c.example.com", "https://d.example.com", "https://x.example.com"),
		liveWindow("w3", "https://e.example.com", "https://f.example.com"),
		liveWindow("w4", "https://c.example.com"),
	}
	want := map[string]uint{"w2": 3, "w3": 4}

	assert.Equal(t, want, matchRestored(rows, windows, nil, 0.5))

	reversedRows := []BrowserWindow{rows[3], rows[2], rows[1], rows[0]}
	reversedWindows := []Window{windows[3], windows[2], windows[1], windows[0]}
	assert.Equal(t, want, matchRestored(reversedRows, reversedWindows, nil, 0.5))
}
