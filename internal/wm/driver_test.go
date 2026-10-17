package wm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNone_IsInert(t *testing.T) {
	ctx := context.Background()
	var d Driver = None{}

	windows, err := d.ListWindows(ctx, "com.google.Chrome")
	require.NoError(t, err)
	assert.Nil(t, windows)
	assert.NoError(t, d.MoveToWorkspace(ctx, "1", "P"))
}

func TestFakeDriver_ListWindows(t *testing.T) {
	f := NewFakeDriver()
	f.Add("1", "a - Chrome", "2")
	f.Add("2", "b - Chrome", "3")

	windows, err := f.ListWindows(context.Background(), "ignored")
	require.NoError(t, err)
	assert.ElementsMatch(t, []Window{
		{ID: "1", Title: "a - Chrome", Workspace: "2"},
		{ID: "2", Title: "b - Chrome", Workspace: "3"},
	}, windows)
}

func TestFakeDriver_SetTitleAndRemove(t *testing.T) {
	f := NewFakeDriver()
	f.Add("1", "a", "2")
	f.Add("2", "b", "2")
	f.SetTitle("1", "renamed")
	f.Remove("2")

	windows, err := f.ListWindows(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, []Window{{ID: "1", Title: "renamed", Workspace: "2"}}, windows)
}

func TestFakeDriver_MoveToWorkspace(t *testing.T) {
	ctx := context.Background()
	f := NewFakeDriver()
	f.Add("1", "a", "2")

	require.NoError(t, f.MoveToWorkspace(ctx, "1", "P"))

	assert.Equal(t, "P", f.Workspace("1"))
	assert.Equal(t, []Move{{WindowID: "1", Workspace: "P"}}, f.Moves())
}

func TestFakeDriver_MoveUnknownWindow(t *testing.T) {
	f := NewFakeDriver()

	assert.Error(t, f.MoveToWorkspace(context.Background(), "missing", "P"))
	assert.Empty(t, f.Moves())
}

func TestFakeDriver_MoveErr(t *testing.T) {
	f := NewFakeDriver()
	f.Add("1", "a", "2")
	f.MoveErr = errors.New("boom")

	assert.ErrorIs(t, f.MoveToWorkspace(context.Background(), "1", "P"), f.MoveErr)
	assert.Equal(t, "2", f.Workspace("1"))
}

func TestNone_FocusWindowIsNoop(t *testing.T) {
	assert.NoError(t, None{}.FocusWindow(context.Background(), "1"))
}

func TestFakeDriver_FocusWindowRecords(t *testing.T) {
	f := NewFakeDriver()
	var seen []string
	f.OnFocus = func(id string) { seen = append(seen, id) }

	require.NoError(t, f.FocusWindow(context.Background(), "1"))
	require.NoError(t, f.FocusWindow(context.Background(), "2"))

	assert.Equal(t, []string{"1", "2"}, f.Focused())
	assert.Equal(t, []string{"1", "2"}, seen)
}

func TestFakeDriver_FocusErr(t *testing.T) {
	f := NewFakeDriver()
	f.FocusErr = errors.New("boom")
	attempts := 0
	f.OnFocus = func(string) { attempts++ }

	assert.ErrorIs(t, f.FocusWindow(context.Background(), "1"), f.FocusErr)
	assert.Empty(t, f.Focused())
	assert.Equal(t, 1, attempts)
}

func TestFakeDriver_ListErr(t *testing.T) {
	f := NewFakeDriver()
	f.ListErr = errors.New("boom")

	_, err := f.ListWindows(context.Background(), "")
	assert.ErrorIs(t, err, f.ListErr)
}

func TestNone_FocusedWindowIsEmpty(t *testing.T) {
	_, ok, err := None{}.FocusedWindow(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFakeDriver_FocusedWindowTracksFocus(t *testing.T) {
	ctx := context.Background()
	f := NewFakeDriver()
	f.Add("1", "a", "2")

	_, ok, err := f.FocusedWindow(ctx)
	require.NoError(t, err)
	assert.False(t, ok)

	f.SetCurrent("term")
	w, ok, err := f.FocusedWindow(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, Window{ID: "term"}, w)

	require.NoError(t, f.FocusWindow(ctx, "1"))
	w, ok, err = f.FocusedWindow(ctx)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, Window{ID: "1", Title: "a", Workspace: "2"}, w)
}
