package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchTab(t *testing.T) {
	pr := "https://github.com/o/r/pull/123"
	tests := []struct {
		name   string
		tabs   []string
		url    string
		wantID string
	}{
		{"exact", []string{pr}, pr, "0"},
		{"tab is deeper", []string{pr + "/files"}, pr, "0"},
		{"trailing slash", []string{pr + "/"}, pr, "0"},
		{"query and fragment ignored", []string{pr + "/files?w=1#diff"}, pr + "#top", "0"},
		{"segment boundary", []string{"https://github.com/o/r/pull/1234"}, pr, ""},
		{"request is deeper", []string{pr}, pr + "/files", ""},
		{"other host", []string{"https://gitlab.com/o/r/pull/123"}, pr, ""},
		{"host case-insensitive", []string{"https://GitHub.com/o/r/pull/123"}, pr, "0"},
		{"exact beats deeper", []string{pr + "/files", pr}, pr, "1"},
		{"root request matches nothing", []string{"https://github.com/x"}, "https://github.com/", ""},
		{"unparseable tab skipped", []string{"::bad", pr}, pr, "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tabs := make([]Tab, len(tt.tabs))
			for i, u := range tt.tabs {
				tabs[i] = Tab{ID: string(rune('0' + i)), URL: u}
			}
			got, ok := MatchTab(tabs, tt.url)
			if tt.wantID == "" {
				assert.False(t, ok)
				return
			}
			assert.True(t, ok)
			assert.Equal(t, tt.wantID, got.ID)
		})
	}
}
