package aerospace

import (
	"context"
	"errors"
	"testing"

	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleJSON = `[{"window-id":70770,"window-title":"Extensions - utena - Google Chrome","workspace":"2"},{"window-id":70977,"window-title":"fix(chrome): polka dots … - Google Chrome","workspace":"3"}]`

type fakeRunner struct {
	calls [][]string
	out   []byte
	err   error
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return r.out, r.err
}

func TestListWindows_Argv(t *testing.T) {
	r := &fakeRunner{out: []byte("[]")}
	d := newDriver(r)

	_, err := d.ListWindows(context.Background(), "com.google.Chrome")
	require.NoError(t, err)

	assert.Equal(t, [][]string{{
		"aerospace", "list-windows", "--monitor", "all", "--app-bundle-id", "com.google.Chrome",
		"--json", "--format", "%{window-id} %{window-title} %{workspace}",
	}}, r.calls)
}

func TestListWindows_DecodesJSON(t *testing.T) {
	d := newDriver(&fakeRunner{out: []byte(sampleJSON)})

	windows, err := d.ListWindows(context.Background(), "com.google.Chrome")
	require.NoError(t, err)

	assert.Equal(t, []wm.Window{
		{ID: "70770", Title: "Extensions - utena - Google Chrome", Workspace: "2"},
		{ID: "70977", Title: "fix(chrome): polka dots … - Google Chrome", Workspace: "3"},
	}, windows)
}

func TestListWindows_RunnerError(t *testing.T) {
	boom := errors.New("boom")
	d := newDriver(&fakeRunner{err: boom})

	_, err := d.ListWindows(context.Background(), "com.google.Chrome")
	assert.ErrorIs(t, err, boom)
}

func TestListWindows_BadJSON(t *testing.T) {
	d := newDriver(&fakeRunner{out: []byte("not json")})

	_, err := d.ListWindows(context.Background(), "com.google.Chrome")
	assert.Error(t, err)
}

func TestListWindows_EmptyBundleID(t *testing.T) {
	r := &fakeRunner{out: []byte("[]")}
	d := newDriver(r)

	_, err := d.ListWindows(context.Background(), "")
	assert.Error(t, err)
	assert.Empty(t, r.calls)
}

func TestMoveToWorkspace_Argv(t *testing.T) {
	r := &fakeRunner{}
	d := newDriver(r)

	require.NoError(t, d.MoveToWorkspace(context.Background(), "70770", "P"))

	assert.Equal(t, [][]string{{"aerospace", "move-node-to-workspace", "--window-id", "70770", "P"}}, r.calls)
}

func TestMoveToWorkspace_RunnerError(t *testing.T) {
	boom := errors.New("boom")
	d := newDriver(&fakeRunner{err: boom})

	assert.ErrorIs(t, d.MoveToWorkspace(context.Background(), "70770", "P"), boom)
}

func TestNewDriver_UsesExecRunner(t *testing.T) {
	d := NewDriver()

	assert.IsType(t, execRunner{}, d.runner)
}

func TestExecRunner_IncludesStderrOnFailure(t *testing.T) {
	_, err := execRunner{}.Run(context.Background(), "sh", "-c", "echo 'no such window' >&2; exit 3")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such window")
	assert.Contains(t, err.Error(), "exit status 3")
}

func TestExecRunner_ReturnsStdout(t *testing.T) {
	out, err := execRunner{}.Run(context.Background(), "sh", "-c", "echo ok")

	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(out))
}

func TestFocusWindow_Argv(t *testing.T) {
	r := &fakeRunner{}
	d := newDriver(r)

	require.NoError(t, d.FocusWindow(context.Background(), "70770"))

	assert.Equal(t, [][]string{{"aerospace", "focus", "--window-id", "70770"}}, r.calls)
}

func TestFocusWindow_RunnerError(t *testing.T) {
	boom := errors.New("boom")
	d := newDriver(&fakeRunner{err: boom})

	assert.ErrorIs(t, d.FocusWindow(context.Background(), "70770"), boom)
}

func TestFocusedWindow_ArgvAndDecode(t *testing.T) {
	r := &fakeRunner{out: []byte(`[{"window-id":65841,"window-title":"utena attach","workspace":"T"}]`)}
	d := newDriver(r)

	w, ok, err := d.FocusedWindow(context.Background())
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, wm.Window{ID: "65841", Title: "utena attach", Workspace: "T"}, w)
	assert.Equal(t, [][]string{{
		"aerospace", "list-windows", "--focused",
		"--json", "--format", "%{window-id} %{window-title} %{workspace}",
	}}, r.calls)
}

func TestFocusedWindow_NoneFocused(t *testing.T) {
	d := newDriver(&fakeRunner{out: []byte("[]")})

	_, ok, err := d.FocusedWindow(context.Background())
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestFocusedWindow_RunnerError(t *testing.T) {
	boom := errors.New("boom")
	d := newDriver(&fakeRunner{err: boom})

	_, ok, err := d.FocusedWindow(context.Background())
	assert.ErrorIs(t, err, boom)
	assert.False(t, ok)
}

func TestFocusedWorkspace_ArgvAndDecode(t *testing.T) {
	r := &fakeRunner{out: []byte(`[{"workspace":"T"}]`)}

	ws, err := newDriver(r).FocusedWorkspace(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "T", ws)
	assert.Equal(t, [][]string{{"aerospace", "list-workspaces", "--focused", "--json"}}, r.calls)
}

func TestFocusedWorkspace_RunnerError(t *testing.T) {
	boom := errors.New("boom")

	_, err := newDriver(&fakeRunner{err: boom}).FocusedWorkspace(context.Background())

	assert.ErrorIs(t, err, boom)
}
