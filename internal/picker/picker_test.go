package picker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/wm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAppID = "com.example.term"
	testTitle = "utena-picker"
	testUtena = "/home/u/.local/bin/utena"
)

type fakeLauncher struct {
	launches [][]string
	err      error
}

func (l *fakeLauncher) Launch(argv []string) error {
	l.launches = append(l.launches, argv)
	return l.err
}

type erroringWM struct {
	wm.FakeDriver
	focusedErr error
}

func (e *erroringWM) FocusedWindow(ctx context.Context) (wm.Window, bool, error) {
	return wm.Window{}, false, e.focusedErr
}

func newOpener(d wm.Driver, l Launcher) Opener {
	return Opener{WM: d, Launcher: l, AppID: testAppID, Title: testTitle, Utena: testUtena, ParkWorkspace: "P"}
}

func readReturn(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

var terminal = []string{"/Applications/Term.app/term", "--title", testTitle}

func TestOpen_FocusesExistingPicker(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("42", testTitle, "T")
	d.SetCurrent("10")
	l := &fakeLauncher{}

	require.NoError(t, newOpener(d, l).Open(context.Background(), terminal))

	assert.Equal(t, []string{"42"}, d.Focused())
	assert.Empty(t, d.Moves())
	assert.Empty(t, l.launches)
}

func TestOpen_UnparksPickerOntoFocusedWorkspace(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("42", testTitle, "P")
	d.SetCurrent("10")
	o := newOpener(d, &fakeLauncher{})
	o.ReturnPath = filepath.Join(t.TempDir(), "utena", "return")

	require.NoError(t, o.Open(context.Background(), terminal))

	assert.Equal(t, []wm.Move{{WindowID: "42", Workspace: "T"}}, d.Moves())
	assert.Equal(t, []string{"42"}, d.Focused())
	assert.Equal(t, "10", readReturn(t, o.ReturnPath))
}

func TestOpen_EmptyWorkspaceUnparksOntoFocusedWorkspace(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("42", testTitle, "P")
	d.SetFocusedWorkspace("3")
	o := newOpener(d, &fakeLauncher{})
	o.ReturnPath = filepath.Join(t.TempDir(), "return")
	require.NoError(t, os.WriteFile(o.ReturnPath, []byte("stale"), 0o600))

	require.NoError(t, o.Open(context.Background(), terminal))

	assert.Equal(t, []wm.Move{{WindowID: "42", Workspace: "3"}}, d.Moves())
	assert.Equal(t, "", readReturn(t, o.ReturnPath))
}

func TestOpen_UnparkFails(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("42", testTitle, "P")
	d.SetCurrent("10")
	d.MoveErr = errors.New("boom")

	assert.Error(t, newOpener(d, &fakeLauncher{}).Open(context.Background(), terminal))
	assert.Empty(t, d.Focused())
}

func TestOpen_PickerAlreadyFocusedIsNoop(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("42", testTitle, "T")
	d.SetCurrent("42")
	l := &fakeLauncher{}

	require.NoError(t, newOpener(d, l).Open(context.Background(), terminal))

	assert.Empty(t, d.Focused())
	assert.Empty(t, l.launches)
}

var launchEnv = []string{"/usr/bin/env", EnvPicker + "=1", EnvTitle + "=" + testTitle, EnvPark + "=P", testUtena}

func TestOpen_LaunchesAndRecordsReturnWindow(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("11", "utena-picker-notes", "T")
	d.SetCurrent("10")
	l := &fakeLauncher{}
	o := newOpener(d, l)
	o.ReturnPath = filepath.Join(t.TempDir(), "return")

	require.NoError(t, o.Open(context.Background(), terminal))

	assert.Empty(t, d.Focused())
	assert.Equal(t, [][]string{append(append([]string(nil), terminal...), launchEnv...)}, l.launches)
	assert.Equal(t, "10", readReturn(t, o.ReturnPath))
}

func TestOpen_NoParkWorkspaceOmitsParkEnv(t *testing.T) {
	l := &fakeLauncher{}
	o := newOpener(wm.NewFakeDriver(), l)
	o.ParkWorkspace = ""

	require.NoError(t, o.Open(context.Background(), terminal))

	require.Len(t, l.launches, 1)
	assert.Equal(t, []string{"/usr/bin/env", EnvPicker + "=1", EnvTitle + "=" + testTitle, testUtena}, l.launches[0][3:])
}

func TestOpen_WMUnavailableStillLaunches(t *testing.T) {
	boom := errors.New("aerospace: not found")
	d := &erroringWM{focusedErr: boom}
	d.ListErr = boom
	l := &fakeLauncher{}

	require.NoError(t, newOpener(d, l).Open(context.Background(), terminal))

	require.Len(t, l.launches, 1)
	assert.Equal(t, launchEnv, l.launches[0][3:])
}

func TestOpen_InertWMLaunches(t *testing.T) {
	l := &fakeLauncher{}

	require.NoError(t, newOpener(wm.None{}, l).Open(context.Background(), terminal))

	assert.Len(t, l.launches, 1)
}

func TestOpen_FocusExistingFails(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("42", testTitle, "T")
	d.FocusErr = errors.New("boom")
	l := &fakeLauncher{}

	assert.Error(t, newOpener(d, l).Open(context.Background(), terminal))
	assert.Empty(t, l.launches)
}

func TestOpen_LaunchError(t *testing.T) {
	boom := errors.New("boom")
	l := &fakeLauncher{err: boom}

	assert.ErrorIs(t, newOpener(wm.NewFakeDriver(), l).Open(context.Background(), terminal), boom)
}

func TestOpen_RequiresTerminalAndTitle(t *testing.T) {
	l := &fakeLauncher{}
	o := newOpener(wm.NewFakeDriver(), l)

	assert.Error(t, o.Open(context.Background(), nil))
	o.Title = ""
	assert.Error(t, o.Open(context.Background(), terminal))
	assert.Empty(t, l.launches)
}

func TestOpen_NoAppIDSkipsLookup(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("42", testTitle, "T")
	l := &fakeLauncher{}
	o := newOpener(d, l)
	o.AppID = ""

	require.NoError(t, o.Open(context.Background(), terminal))

	assert.Empty(t, d.Focused())
	assert.Len(t, l.launches, 1)
}

func TestMatchWindow(t *testing.T) {
	windows := []wm.Window{
		{ID: "1", Title: "utena-picker - zsh"},
		{ID: "2", Title: "Utena-Picker"},
		{ID: "3", Title: testTitle},
	}

	w, ok := MatchWindow(windows, testTitle)
	assert.True(t, ok)
	assert.Equal(t, "3", w.ID)

	_, ok = MatchWindow(windows[:2], testTitle)
	assert.False(t, ok)
}

func TestLaunchArgvDoesNotAliasTerminal(t *testing.T) {
	term := make([]string, 1, 8)
	term[0] = "term"

	a := LaunchArgv(term, testUtena, testTitle, "1")
	b := LaunchArgv(term, testUtena, testTitle, "2")

	assert.Equal(t, EnvPark+"=1", a[len(a)-2])
	assert.Equal(t, EnvPark+"=2", b[len(b)-2])
}

func writeReturn(t *testing.T, id string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "return")
	require.NoError(t, os.WriteFile(path, []byte(id), 0o600))
	return path
}

func TestReturnFocus_FocusesRecordedWindow(t *testing.T) {
	d := wm.NewFakeDriver()

	require.NoError(t, ReturnFocus(context.Background(), d, writeReturn(t, "10\n")))

	assert.Equal(t, []string{"10"}, d.Focused())
}

func TestReturnFocus_NothingRecorded(t *testing.T) {
	d := wm.NewFakeDriver()

	require.NoError(t, ReturnFocus(context.Background(), d, filepath.Join(t.TempDir(), "missing")))
	require.NoError(t, ReturnFocus(context.Background(), d, writeReturn(t, "")))

	assert.Empty(t, d.Focused())
}

func TestReturnFocus_Error(t *testing.T) {
	d := wm.NewFakeDriver()
	d.FocusErr = errors.New("boom")

	assert.Error(t, ReturnFocus(context.Background(), d, writeReturn(t, "10")))
}

func TestPark_MovesPickerAndReturnsFocus(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("42", testTitle, "T")
	d.SetCurrent("42")

	require.NoError(t, Park(context.Background(), d, testTitle, "P", writeReturn(t, "10")))

	assert.Equal(t, []wm.Move{{WindowID: "42", Workspace: "P"}}, d.Moves())
	assert.Equal(t, []string{"10"}, d.Focused())
}

func TestPark_LeavesOtherFocusedWindowAlone(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("10", "tuios", "T")
	d.Add("50", "chrome", "T")
	d.SetCurrent("50")

	require.NoError(t, Park(context.Background(), d, testTitle, "P", writeReturn(t, "10")))

	assert.Empty(t, d.Moves())
	assert.Equal(t, []string{"10"}, d.Focused())
}

func TestPark_MoveError(t *testing.T) {
	d := wm.NewFakeDriver()
	d.Add("42", testTitle, "T")
	d.SetCurrent("42")
	d.MoveErr = errors.New("boom")

	assert.Error(t, Park(context.Background(), d, testTitle, "P", writeReturn(t, "10")))
	assert.Empty(t, d.Focused())
}

func TestExecLauncher_StartsDetached(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "ran")

	require.NoError(t, ExecLauncher{}.Launch([]string{"sh", "-c", "touch " + marker}))

	assert.Eventually(t, func() bool {
		_, err := os.Stat(marker)
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)
}

func TestExecLauncher_MissingBinary(t *testing.T) {
	assert.Error(t, ExecLauncher{}.Launch([]string{filepath.Join(t.TempDir(), "missing")}))
}
