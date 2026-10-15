package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSavedTabs(t *testing.T) {
	w := Window{ID: "w1", Tabs: []Tab{
		{ID: "1", URL: "https://a.example", Title: "A", Pinned: true},
		{ID: "2", URL: "chrome://newtab/"},
		{ID: "3", URL: ""},
		{ID: "4", URL: "https://b.example", Title: "B", Active: true},
	}}

	assert.Equal(t, []SavedTab{
		{URL: "https://a.example", Title: "A", Pinned: true},
		{URL: "https://b.example", Title: "B", Active: true},
	}, savedTabs(w))
}

func TestSavedTabsEmpty(t *testing.T) {
	assert.Nil(t, savedTabs(Window{ID: "w1", Tabs: []Tab{{ID: "1", URL: "chrome://newtab/"}}}))
}
