package chrome

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
)

func TestDriver(t *testing.T) {
	d := NewDriver()
	assert.Equal(t, "chrome", d.Name())
	assert.Equal(t, "com.google.Chrome", d.AppBundleID())
	w := browser.Window{ID: "1", Tabs: []browser.Tab{{ID: "2", Title: "Extensions - utena", Active: true}}}
	assert.Equal(t, "Extensions - utena - Google Chrome", d.WindowTitle(w))
}
