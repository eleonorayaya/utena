package chromescript

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/eleonorayaya/utena/internal/chrome"
)

const Name = "chrome-applescript"

const (
	pollInterval   = 1500 * time.Millisecond
	callTimeout    = 3 * time.Second
	maxPollErrors  = 3
	recordSep      = "\x1e"
	unitSep        = "\x1f"
	automationHint = "allow utena to control Chrome in System Settings → Privacy & Security → Automation"
)

var errNotAuthorized = errors.New("chromescript: not allowed to control Chrome")

type Runner interface {
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return out, fmt.Errorf("%w: %s", err, bytes.TrimSpace(exitErr.Stderr))
	}
	return out, err
}

type Driver struct {
	runner   Runner
	interval time.Duration

	mu      sync.Mutex
	warning string
}

var (
	_ browser.Driver             = (*Driver)(nil)
	_ browser.CapabilityReporter = (*Driver)(nil)
	_ browser.WarningReporter    = (*Driver)(nil)
)

func NewDriver() *Driver {
	return newDriver(execRunner{}, pollInterval)
}

func newDriver(r Runner, interval time.Duration) *Driver {
	return &Driver{runner: r, interval: interval}
}

func (d *Driver) Capabilities() browser.Capabilities {
	return browser.Capabilities{CreateTakesFocus: true, LoadTakesFocus: true}
}

func (d *Driver) Name() string {
	return Name
}

func (d *Driver) AppBundleID() string {
	return chrome.Browser.BundleID
}

func (d *Driver) WindowTitle(w browser.Window) string {
	t, ok := w.ActiveTab()
	if !ok || t.Title == "" {
		return ""
	}
	return t.Title + chrome.Browser.TitleSuffix
}

func (d *Driver) Warning() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.warning
}

func (d *Driver) setWarning(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch {
	case errors.Is(err, errNotAuthorized):
		d.warning = automationHint
	case err == nil:
		d.warning = ""
	}
}

func (d *Driver) osascript(ctx context.Context, script string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := d.runner.Run(ctx, "osascript", append([]string{"-e", script}, args...)...)
	if err != nil {
		if strings.Contains(err.Error(), "-1743") {
			err = fmt.Errorf("%w: %w", errNotAuthorized, err)
		}
		d.setWarning(err)
		return "", fmt.Errorf("osascript: %w", err)
	}
	d.setWarning(nil)
	return strings.TrimRight(string(out), "\n"), nil
}

type snapshot struct {
	running   bool
	frontmost bool
	windows   []browser.Window
}

func (s snapshot) focusedID() string {
	if !s.frontmost {
		return ""
	}
	for _, w := range s.windows {
		if w.Focused {
			return w.ID
		}
	}
	return ""
}

const pollScript = `on run
	if application id "com.google.Chrome" is not running then return "down"
	set RS to character id 30
	set US to character id 31
	tell application id "com.google.Chrome"
		set fm to frontmost
		set wids to id of every window whose mode is "normal"
		set widx to index of every window whose mode is "normal"
		set wact to active tab index of every window whose mode is "normal"
		set tids to id of every tab of (every window whose mode is "normal")
		set turls to URL of every tab of (every window whose mode is "normal")
		set ttitles to title of every tab of (every window whose mode is "normal")
	end tell
	set out to "up" & US & (fm as text)
	repeat with i from 1 to count of wids
		set out to out & RS & "w" & US & (item i of wids) & US & (item i of widx) & US & (item i of wact)
		set ti to item i of tids
		set tu to item i of turls
		set tt to item i of ttitles
		repeat with j from 1 to count of ti
			set out to out & RS & "t" & US & (item j of ti) & US & (item j of tu) & US & (item j of tt)
		end repeat
	end repeat
	return out
end run`

func (d *Driver) poll(ctx context.Context) (snapshot, error) {
	out, err := d.osascript(ctx, pollScript)
	if err != nil {
		return snapshot{}, err
	}
	return parseSnapshot(out)
}

func parseSnapshot(out string) (snapshot, error) {
	if out == "down" {
		return snapshot{}, nil
	}
	records := strings.Split(out, recordSep)
	head := strings.Split(records[0], unitSep)
	if len(head) != 2 || head[0] != "up" {
		return snapshot{}, fmt.Errorf("chromescript: unexpected poll output %q", records[0])
	}
	s := snapshot{running: true, frontmost: head[1] == "true"}
	activeIndex := 0
	for _, rec := range records[1:] {
		f := strings.Split(rec, unitSep)
		switch {
		case f[0] == "w" && len(f) == 4:
			index, err := strconv.Atoi(f[2])
			if err != nil {
				return snapshot{}, fmt.Errorf("chromescript: bad window index %q", f[2])
			}
			if activeIndex, err = strconv.Atoi(f[3]); err != nil {
				return snapshot{}, fmt.Errorf("chromescript: bad active tab index %q", f[3])
			}
			s.windows = append(s.windows, browser.Window{ID: f[1], Focused: s.frontmost && index == 1})
		case f[0] == "t" && len(f) >= 4 && len(s.windows) > 0:
			w := &s.windows[len(s.windows)-1]
			w.Tabs = append(w.Tabs, browser.Tab{ID: f[1], URL: f[2], Title: strings.Join(f[3:], unitSep), Active: len(w.Tabs)+1 == activeIndex})
		default:
			return snapshot{}, fmt.Errorf("chromescript: unexpected poll record %q", rec)
		}
	}
	return s, nil
}

func (d *Driver) running(ctx context.Context) (snapshot, error) {
	s, err := d.poll(ctx)
	if err != nil {
		return snapshot{}, err
	}
	if !s.running {
		return snapshot{}, browser.ErrNotConnected
	}
	return s, nil
}

func (d *Driver) ListWindows(ctx context.Context) ([]browser.Window, error) {
	s, err := d.running(ctx)
	if err != nil {
		return nil, err
	}
	return s.windows, nil
}

const createScript = `on run argv
	tell application id "com.google.Chrome"
		set w to make new window
		if (count of argv) > 0 then
			set URL of active tab of w to item 1 of argv
			repeat with i from 2 to count of argv
				tell w to make new tab with properties {URL:(item i of argv)}
			end repeat
		end if
		return id of w
	end tell
end run`

func (d *Driver) CreateWindow(ctx context.Context, urls []string, focused bool) (browser.Window, error) {
	if _, err := d.running(ctx); err != nil {
		return browser.Window{}, err
	}
	id, err := d.osascript(ctx, createScript, urls...)
	if err != nil {
		return browser.Window{}, err
	}
	return d.window(ctx, id)
}

func (d *Driver) window(ctx context.Context, id string) (browser.Window, error) {
	windows, err := d.ListWindows(ctx)
	if err != nil {
		return browser.Window{}, err
	}
	for _, w := range windows {
		if w.ID == id {
			return w, nil
		}
	}
	return browser.Window{ID: id}, nil
}

const openTabScript = `on run argv
	tell application id "com.google.Chrome"
		tell window id ((item 1 of argv) as integer)
			set t to make new tab with properties {URL:(item 2 of argv)}
			return id of t
		end tell
	end tell
end run`

func (d *Driver) OpenTab(ctx context.Context, windowID, url string) (browser.Tab, error) {
	id, err := d.osascript(ctx, openTabScript, windowID, url)
	if err != nil {
		return browser.Tab{}, err
	}
	return browser.Tab{ID: id, URL: url, Active: true}, nil
}

const activateTabScript = `on run argv
	tell application id "com.google.Chrome"
		tell window id ((item 1 of argv) as integer)
			set tids to id of every tab
			repeat with i from 1 to count of tids
				if ((item i of tids) as text) is (item 2 of argv) then
					set active tab index to i
					return "ok"
				end if
			end repeat
		end tell
	end tell
	return "missing"
end run`

func (d *Driver) ActivateTab(ctx context.Context, windowID, tabID string) error {
	out, err := d.osascript(ctx, activateTabScript, windowID, tabID)
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("chromescript: no tab %s in window %s", tabID, windowID)
	}
	return nil
}

const focusScript = `on run argv
	tell application id "com.google.Chrome"
		set index of window id ((item 1 of argv) as integer) to 1
		activate
	end tell
end run`

func (d *Driver) FocusWindow(ctx context.Context, windowID string) error {
	_, err := d.osascript(ctx, focusScript, windowID)
	return err
}

const closeScript = `on run argv
	tell application id "com.google.Chrome" to close window id ((item 1 of argv) as integer)
end run`

func (d *Driver) CloseWindow(ctx context.Context, windowID string) error {
	_, err := d.osascript(ctx, closeScript, windowID)
	return err
}

func (d *Driver) runID(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	out, err := d.runner.Run(ctx, "pgrep", "-x", "Google Chrome")
	if err != nil {
		return ""
	}
	pid, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return pid
}

func (d *Driver) Subscribe(ctx context.Context, onEvent func(browser.Event)) error {
	prev, err := d.waitRunning(ctx)
	if err != nil {
		return err
	}
	run := d.runID(ctx)
	onEvent(browser.Event{Type: browser.EventConnected, Run: run})
	var removed []browser.Event
	failures := 0
	for {
		if !sleepCtx(ctx, d.interval) {
			return ctx.Err()
		}
		cur, err := d.poll(ctx)
		if err != nil {
			failures++
			if failures >= maxPollErrors {
				return err
			}
			continue
		}
		failures = 0
		if !cur.running || (run != "" && d.runID(ctx) != run) {
			return browser.ErrNotConnected
		}
		for _, ev := range removed {
			onEvent(ev)
		}
		removed = nil
		for _, ev := range diff(prev, cur) {
			if ev.Type == browser.EventWindowRemoved {
				removed = append(removed, ev)
				continue
			}
			onEvent(ev)
		}
		prev = cur
	}
}

func (d *Driver) waitRunning(ctx context.Context) (snapshot, error) {
	failures := 0
	for {
		s, err := d.poll(ctx)
		switch {
		case err != nil:
			failures++
			if failures >= maxPollErrors {
				return snapshot{}, err
			}
		case s.running:
			return s, nil
		default:
			failures = 0
		}
		if !sleepCtx(ctx, d.interval) {
			return snapshot{}, ctx.Err()
		}
	}
}

func diff(prev, cur snapshot) []browser.Event {
	before := make(map[string]browser.Window, len(prev.windows))
	for _, w := range prev.windows {
		before[w.ID] = w
	}
	var events []browser.Event
	seen := make(map[string]bool, len(cur.windows))
	for _, w := range cur.windows {
		seen[w.ID] = true
		old, ok := before[w.ID]
		switch {
		case !ok:
			events = append(events, browser.Event{Type: browser.EventWindowCreated, WindowID: w.ID})
		case !sameTabs(old.Tabs, w.Tabs):
			events = append(events, browser.Event{Type: browser.EventTabsChanged, WindowID: w.ID})
		}
	}
	for _, w := range prev.windows {
		if !seen[w.ID] {
			events = append(events, browser.Event{Type: browser.EventWindowRemoved, WindowID: w.ID})
		}
	}
	if id := cur.focusedID(); id != "" && id != prev.focusedID() {
		events = append(events, browser.Event{Type: browser.EventFocusChanged, WindowID: id})
	}
	return events
}

func sameTabs(a, b []browser.Tab) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
