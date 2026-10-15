package browser

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFakeDriver_AppBundleID(t *testing.T) {
	assert.Equal(t, "fake.browser", NewFakeDriver().AppBundleID())
}

func TestFakeDriver_WindowTitle(t *testing.T) {
	f := NewFakeDriver()
	w := f.AddWindow("https://a.example", "https://b.example")

	f.SetTitle(w.ID, "B")

	got, ok := f.Window(w.ID)
	assert.True(t, ok)
	assert.Equal(t, "B", got.Tabs[1].Title)
	assert.Equal(t, "", got.Tabs[0].Title)
	assert.Equal(t, "B - Fake", f.WindowTitle(got))
}

func TestFakeDriver_WindowTitleWithoutActiveTitle(t *testing.T) {
	f := NewFakeDriver()

	assert.Equal(t, "", f.WindowTitle(Window{ID: "w1"}))
	assert.Equal(t, "", f.WindowTitle(f.AddWindow("https://a.example")))
}
