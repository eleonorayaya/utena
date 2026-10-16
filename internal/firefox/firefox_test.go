package firefox

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
)

func TestDriver(t *testing.T) {
	d := NewDriver()
	assert.Equal(t, "firefox", d.Name())
	assert.Equal(t, "org.mozilla.firefox", d.AppBundleID())
	w := browser.Window{ID: "1", Tabs: []browser.Tab{{ID: "2", Title: "X2S Honeywell Smart Thermostat", Active: true}}}
	assert.Equal(t, "X2S Honeywell Smart Thermostat", d.WindowTitle(w))
}
