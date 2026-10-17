package chromescript

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type call struct {
	name string
	args []string
}

type fakeRunner struct {
	mu      sync.Mutex
	calls   []call
	polls   []string
	pollAt  int
	pollErr error
	reply   map[string]string
	err     error
	pids    []string
	pidAt   int
}

func (r *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{name: name, args: args})
	if name == "pgrep" {
		if len(r.pids) == 0 {
			return []byte("4242\n"), nil
		}
		pid := r.pids[min(r.pidAt, len(r.pids)-1)]
		r.pidAt++
		return []byte(pid + "\n"), nil
	}
	script := args[1]
	if script == pollScript {
		if r.pollErr != nil {
			return nil, r.pollErr
		}
		if len(r.polls) == 0 {
			return []byte("down\n"), nil
		}
		out := r.polls[min(r.pollAt, len(r.polls)-1)]
		r.pollAt++
		return []byte(out + "\n"), nil
	}
	if r.err != nil {
		return nil, r.err
	}
	for marker, out := range r.reply {
		if strings.Contains(script, marker) {
			return []byte(out + "\n"), nil
		}
	}
	return nil, nil
}

func (r *fakeRunner) scripts() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []call
	for _, c := range r.calls {
		if c.name == "osascript" && c.args[1] != pollScript {
			out = append(out, c)
		}
	}
	return out
}

func up(frontmost bool, windows ...string) string {
	head := "up" + unitSep + map[bool]string{true: "true", false: "false"}[frontmost]
	return strings.Join(append([]string{head}, windows...), recordSep)
}

func win(id, index, active string, tabs ...string) string {
	return strings.Join(append([]string{"w" + unitSep + id + unitSep + index + unitSep + active}, tabs...), recordSep)
}

func tab(id, url, title string) string {
	return "t" + unitSep + id + unitSep + url + unitSep + title
}

func TestParseSnapshot(t *testing.T) {
	s, err := parseSnapshot(up(true,
		win("10", "1", "2", tab("1", "https://a.example.com", "A"), tab("2", "https://b.example.com", "B")),
		win("11", "2", "1", tab("3", "chrome://newtab/", "New Tab")),
	))
	require.NoError(t, err)

	assert.True(t, s.running)
	assert.Equal(t, []browser.Window{
		{ID: "10", Focused: true, Tabs: []browser.Tab{
			{ID: "1", URL: "https://a.example.com", Title: "A"},
			{ID: "2", URL: "https://b.example.com", Title: "B", Active: true},
		}},
		{ID: "11", Tabs: []browser.Tab{{ID: "3", URL: "chrome://newtab/", Title: "New Tab", Active: true}}},
	}, s.windows)
	assert.Equal(t, "10", s.focusedID())
}

func TestParseSnapshot_NotFrontmostHasNoFocusedWindow(t *testing.T) {
	s, err := parseSnapshot(up(false, win("10", "1", "1", tab("1", "https://a.example.com", "A"))))
	require.NoError(t, err)
	assert.False(t, s.windows[0].Focused)
	assert.Empty(t, s.focusedID())
}

func TestParseSnapshot_Down(t *testing.T) {
	s, err := parseSnapshot("down")
	require.NoError(t, err)
	assert.False(t, s.running)
}

func TestParseSnapshot_TitleWithSeparatorSurvives(t *testing.T) {
	s, err := parseSnapshot(up(true, win("10", "1", "1", tab("1", "https://a.example.com", "A"+unitSep+"B"))))
	require.NoError(t, err)
	assert.Equal(t, "A"+unitSep+"B", s.windows[0].Tabs[0].Title)
}

func TestParseSnapshot_RejectsGarbage(t *testing.T) {
	_, err := parseSnapshot("what")
	assert.Error(t, err)
	_, err = parseSnapshot(up(true, "x"+unitSep+"1"))
	assert.Error(t, err)
	_, err = parseSnapshot(up(true, win("10", "one", "1")))
	assert.Error(t, err)
}

func TestDiff(t *testing.T) {
	prev := snapshot{running: true, frontmost: true, windows: []browser.Window{
		{ID: "1", Focused: true, Tabs: []browser.Tab{{ID: "a", URL: "https://a"}}},
		{ID: "2", Tabs: []browser.Tab{{ID: "b", URL: "https://b"}}},
		{ID: "3", Tabs: []browser.Tab{{ID: "c", URL: "https://c"}}},
	}}
	cur := snapshot{running: true, frontmost: true, windows: []browser.Window{
		{ID: "1", Tabs: []browser.Tab{{ID: "a", URL: "https://a"}}},
		{ID: "2", Focused: true, Tabs: []browser.Tab{{ID: "b", URL: "https://b2"}}},
		{ID: "4"},
	}}

	assert.Equal(t, []browser.Event{
		{Type: browser.EventTabsChanged, WindowID: "2"},
		{Type: browser.EventWindowCreated, WindowID: "4"},
		{Type: browser.EventWindowRemoved, WindowID: "3"},
		{Type: browser.EventFocusChanged, WindowID: "2"},
	}, diff(prev, cur))
}

func TestDiff_LosingFrontmostIsNotAFocusChange(t *testing.T) {
	prev := snapshot{running: true, frontmost: true, windows: []browser.Window{{ID: "1", Focused: true}}}
	cur := snapshot{running: true, frontmost: false, windows: []browser.Window{{ID: "1"}}}
	assert.Empty(t, diff(prev, cur))

	assert.Equal(t, []browser.Event{{Type: browser.EventFocusChanged, WindowID: "1"}}, diff(cur, prev))
}

func TestListWindows_NotRunningIsNotConnected(t *testing.T) {
	d := newDriver(&fakeRunner{}, time.Millisecond)
	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
}

func TestCreateWindow_PassesURLsAndReturnsWindow(t *testing.T) {
	r := &fakeRunner{
		polls: []string{up(true, win("7", "1", "1", tab("1", "https://a", "A"), tab("2", "https://b", "B")))},
		reply: map[string]string{"make new window": "7"},
	}
	d := newDriver(r, time.Millisecond)

	w, err := d.CreateWindow(context.Background(), []string{"https://a", "https://b"}, false)
	require.NoError(t, err)

	assert.Equal(t, "7", w.ID)
	assert.Len(t, w.Tabs, 2)
	scripts := r.scripts()
	require.Len(t, scripts, 1)
	assert.Equal(t, []string{"https://a", "https://b"}, scripts[0].args[2:])
}

func TestCreateWindow_NotRunningDoesNotLaunchChrome(t *testing.T) {
	r := &fakeRunner{}
	d := newDriver(r, time.Millisecond)

	_, err := d.CreateWindow(context.Background(), nil, false)
	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Empty(t, r.scripts())
}

func TestActions_PassWindowAndTabIDs(t *testing.T) {
	r := &fakeRunner{reply: map[string]string{"make new tab": "99", "set active tab index": "ok"}}
	d := newDriver(r, time.Millisecond)
	ctx := context.Background()

	tb, err := d.OpenTab(ctx, "7", "https://x")
	require.NoError(t, err)
	assert.Equal(t, browser.Tab{ID: "99", URL: "https://x", Active: true}, tb)
	require.NoError(t, d.ActivateTab(ctx, "7", "99"))
	require.NoError(t, d.FocusWindow(ctx, "7"))
	require.NoError(t, d.CloseWindow(ctx, "7"))

	scripts := r.scripts()
	require.Len(t, scripts, 4)
	assert.Equal(t, []string{"7", "https://x"}, scripts[0].args[2:])
	assert.Equal(t, []string{"7", "99"}, scripts[1].args[2:])
	assert.Equal(t, []string{"7"}, scripts[2].args[2:])
	assert.Equal(t, []string{"7"}, scripts[3].args[2:])
}

func TestActivateTab_MissingTab(t *testing.T) {
	d := newDriver(&fakeRunner{reply: map[string]string{"set active tab index": "missing"}}, time.Millisecond)
	assert.Error(t, d.ActivateTab(context.Background(), "7", "99"))
}

func TestAutomationDenialSetsWarning(t *testing.T) {
	r := &fakeRunner{err: errors.New("execution error: Not authorized to send Apple events to Google Chrome. (-1743)")}
	d := newDriver(r, time.Millisecond)

	err := d.FocusWindow(context.Background(), "7")
	assert.ErrorIs(t, err, errNotAuthorized)
	assert.Equal(t, automationHint, d.Warning())

	r.err = nil
	require.NoError(t, d.FocusWindow(context.Background(), "7"))
	assert.Empty(t, d.Warning())
}

type recorder struct {
	mu     sync.Mutex
	events []browser.Event
}

func (r *recorder) add(ev browser.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) all() []browser.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]browser.Event(nil), r.events...)
}

func TestSubscribe_ConnectsWithPIDAndEmitsDiffs(t *testing.T) {
	r := &fakeRunner{polls: []string{
		up(false, win("1", "1", "1", tab("a", "https://a", "A"))),
		up(false, win("1", "1", "1", tab("a", "https://a", "A"))),
		up(true, win("1", "1", "1", tab("a", "https://a", "A")), win("2", "2", "1")),
	}}
	d := newDriver(r, time.Millisecond)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Subscribe(ctx, rec.add) }()

	require.Eventually(t, func() bool { return len(rec.all()) >= 3 }, time.Second, time.Millisecond)
	cancel()
	<-done
	assert.Equal(t, []browser.Event{
		{Type: browser.EventConnected, Run: "4242"},
		{Type: browser.EventWindowCreated, WindowID: "2"},
		{Type: browser.EventFocusChanged, WindowID: "1"},
	}, rec.all()[:3])
}

func TestSubscribe_ReturnsWhenChromeQuits(t *testing.T) {
	r := &fakeRunner{polls: []string{up(false), "down"}}
	d := newDriver(r, time.Millisecond)
	rec := &recorder{}

	err := d.Subscribe(context.Background(), rec.add)

	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Equal(t, []browser.Event{{Type: browser.EventConnected, Run: "4242"}}, rec.all())
}

func TestSubscribe_ReportsRemovedWindowAfterChromeStillRuns(t *testing.T) {
	r := &fakeRunner{polls: []string{
		up(false, win("1", "1", "1"), win("2", "2", "1")),
		up(false, win("1", "1", "1")),
		up(false, win("1", "1", "1")),
	}}
	d := newDriver(r, time.Millisecond)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- d.Subscribe(ctx, rec.add) }()

	require.Eventually(t, func() bool { return len(rec.all()) >= 2 }, time.Second, time.Millisecond)
	cancel()
	<-done
	assert.Equal(t, []browser.Event{
		{Type: browser.EventConnected, Run: "4242"},
		{Type: browser.EventWindowRemoved, WindowID: "2"},
	}, rec.all())
}

func TestSubscribe_WindowsClosingAsChromeQuitsAreNotReportedRemoved(t *testing.T) {
	r := &fakeRunner{polls: []string{
		up(false, win("1", "1", "1"), win("2", "2", "1")),
		up(false, win("1", "1", "1")),
		"down",
	}}
	d := newDriver(r, time.Millisecond)
	rec := &recorder{}

	err := d.Subscribe(context.Background(), rec.add)

	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Equal(t, []browser.Event{{Type: browser.EventConnected, Run: "4242"}}, rec.all())
}

func TestSubscribe_ReturnsWhenChromeRestartsBetweenPolls(t *testing.T) {
	r := &fakeRunner{polls: []string{up(false)}, pids: []string{"4242", "4242", "5151"}}
	d := newDriver(r, time.Millisecond)
	rec := &recorder{}

	err := d.Subscribe(context.Background(), rec.add)

	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Equal(t, []browser.Event{{Type: browser.EventConnected, Run: "4242"}}, rec.all())
}

func TestSubscribe_ToleratesTransientPollErrors(t *testing.T) {
	r := &fakeRunner{pollErr: errors.New("boom")}
	d := newDriver(r, time.Millisecond)

	err := d.Subscribe(context.Background(), func(browser.Event) { t.Fatal("unexpected event") })

	assert.ErrorContains(t, err, "boom")
	r.mu.Lock()
	defer r.mu.Unlock()
	polls := 0
	for _, c := range r.calls {
		if c.name == "osascript" {
			polls++
		}
	}
	assert.Equal(t, maxPollErrors, polls)
}

func TestCapabilities(t *testing.T) {
	assert.Equal(t, browser.Capabilities{CreateTakesFocus: true, LoadTakesFocus: true}, NewDriver().Capabilities())
	assert.Equal(t, "com.google.Chrome", NewDriver().AppBundleID())
	assert.Equal(t, "A - Google Chrome", NewDriver().WindowTitle(browser.Window{Tabs: []browser.Tab{{Title: "A", Active: true}}}))
}
