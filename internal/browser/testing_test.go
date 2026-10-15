package browser

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestFakeDriver_CloseWindow(t *testing.T) {
	f := NewFakeDriver()
	a := f.AddWindow("https://a.example")
	b := f.AddWindow("https://b.example")
	var hooked []string
	f.OnClose = func(id string) { hooked = append(hooked, id) }

	require.NoError(t, f.CloseWindow(context.Background(), a.ID))

	_, ok := f.Window(a.ID)
	assert.False(t, ok)
	_, ok = f.Window(b.ID)
	assert.True(t, ok)
	assert.Equal(t, []string{a.ID}, f.Closed())
	assert.Equal(t, []string{a.ID}, hooked)
}

func TestFakeDriver_CloseUnknownWindow(t *testing.T) {
	f := NewFakeDriver()

	assert.Error(t, f.CloseWindow(context.Background(), "nope"))
	assert.Empty(t, f.Closed())
}
