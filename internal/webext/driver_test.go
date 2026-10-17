package webext

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/browser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testBrowser = Browser{Name: "chrome", BundleID: "com.google.Chrome", OriginScheme: "chrome-extension://", UpdateHint: "reload it", TitleSuffix: " - Google Chrome"}

type incoming struct {
	Type   string          `json:"type"`
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type fakeExt struct {
	ws     *websocket.Conn
	handle func(method string, params json.RawMessage) (any, string)
}

func dial(t *testing.T, srv *httptest.Server, origin string) (*websocket.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}}})
	return ws, err
}

func startExt(t *testing.T, srv *httptest.Server, protocol int, handle func(string, json.RawMessage) (any, string)) *fakeExt {
	t.Helper()
	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws, handle: handle}
	ext.send(t, map[string]any{"type": "hello", "protocol": protocol, "browser": "chrome"})
	go ext.serve()
	return ext
}

func (e *fakeExt) send(t *testing.T, v any) {
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NoError(t, e.ws.Write(context.Background(), websocket.MessageText, data))
}

func (e *fakeExt) serve() {
	for {
		_, data, err := e.ws.Read(context.Background())
		if err != nil {
			return
		}
		var m incoming
		if json.Unmarshal(data, &m) != nil || m.Type != "req" || e.handle == nil {
			continue
		}
		result, errMsg := e.handle(m.Method, m.Params)
		out := map[string]any{"type": "res", "id": m.ID}
		if errMsg != "" {
			out["error"] = errMsg
		} else {
			out["result"] = result
		}
		data, _ = json.Marshal(out)
		_ = e.ws.Write(context.Background(), websocket.MessageText, data)
	}
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

func (r *recorder) has(want browser.Event) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ev := range r.events {
		if ev == want {
			return true
		}
	}
	return false
}

func (r *recorder) connectionEvents() []browser.EventType {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []browser.EventType
	for _, ev := range r.events {
		if ev.Type == browser.EventConnected || ev.Type == browser.EventDisconnected {
			out = append(out, ev.Type)
		}
	}
	return out
}

func newDriver(t *testing.T) (*Driver, *httptest.Server, *recorder) {
	t.Helper()
	d := NewDriver(testBrowser)
	srv := httptest.NewServer(d.Handler())
	t.Cleanup(srv.Close)
	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = d.Subscribe(ctx, rec.add) }()
	return d, srv, rec
}

func newBlockedDriver(t *testing.T) (*Driver, *httptest.Server, *recorder, chan struct{}) {
	t.Helper()
	d := NewDriver(testBrowser)
	srv := httptest.NewServer(d.Handler())
	t.Cleanup(srv.Close)
	rec := &recorder{}
	gate := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() {
		_ = d.Subscribe(ctx, func(ev browser.Event) {
			rec.add(ev)
			if ev.Type == browser.EventWindowCreated {
				select {
				case <-gate:
				case <-ctx.Done():
				}
			}
		})
	}()
	return d, srv, rec, gate
}

func waitConnected(t *testing.T, rec *recorder) {
	t.Helper()
	require.Eventually(t, func() bool { return rec.has(browser.Event{Type: browser.EventConnected}) }, 5*time.Second, 5*time.Millisecond)
}

func fillBuffer(t *testing.T, d *Driver, ext *fakeExt) {
	t.Helper()
	for i := range cap(d.events) + 10 {
		ext.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": i + 1}})
	}
	require.Eventually(t, func() bool { return len(d.events) == cap(d.events) }, 5*time.Second, 5*time.Millisecond)
}

func waitDropped(t *testing.T, d *Driver) {
	t.Helper()
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.conn == nil
	}, 5*time.Second, 5*time.Millisecond)
}

func TestDriver_RejectsNonExtensionOrigin(t *testing.T) {
	_, srv, _ := newDriver(t)
	_, err := dial(t, srv, "https://evil.example.com")
	assert.Error(t, err)
}

func TestDriver_RejectsOtherBrowsersExtension(t *testing.T) {
	_, srv, rec := newDriver(t)
	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws}
	ext.send(t, map[string]any{"type": "hello", "protocol": ProtocolVersion, "browser": "firefox"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for err == nil {
		_, _, err = ws.Read(ctx)
	}
	assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
	assert.False(t, rec.has(browser.Event{Type: browser.EventConnected}))
}

func TestDriver_CallWithoutConnection(t *testing.T) {
	d, _, _ := newDriver(t)
	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
}

func TestDriver_RejectsProtocolMismatch(t *testing.T) {
	for _, protocol := range []int{1, ProtocolVersion + 1} {
		t.Run(strconv.Itoa(protocol), func(t *testing.T) {
			d, srv, rec := newDriver(t)
			ws, err := dial(t, srv, "chrome-extension://abc")
			require.NoError(t, err)
			t.Cleanup(func() { _ = ws.CloseNow() })
			ext := &fakeExt{ws: ws}
			ext.send(t, map[string]any{"type": "hello", "protocol": protocol, "browser": "chrome"})

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for err == nil {
				_, _, err = ws.Read(ctx)
			}
			assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
			assert.False(t, rec.has(browser.Event{Type: browser.EventConnected}))
			_, err = d.ListWindows(context.Background())
			assert.ErrorIs(t, err, browser.ErrNotConnected)
			require.Eventually(t, d.Outdated, 5*time.Second, 5*time.Millisecond)

			startExt(t, srv, ProtocolVersion, nil)
			waitConnected(t, rec)
			assert.False(t, d.Outdated())
		})
	}
}

func TestDriver_MismatchWhileConnectedDoesNotSetOutdated(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws}
	ext.send(t, map[string]any{"type": "hello", "protocol": ProtocolVersion + 1, "browser": "chrome"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for err == nil {
		_, _, err = ws.Read(ctx)
	}
	assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
	assert.False(t, d.Outdated())
}

func TestDriver_NonProtocolHelloFailureDoesNotSetOutdated(t *testing.T) {
	d, srv, _ := newDriver(t)
	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws}
	ext.send(t, map[string]any{"type": "event", "event": "window.created"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for err == nil {
		_, _, err = ws.Read(ctx)
	}
	assert.Equal(t, websocket.StatusPolicyViolation, websocket.CloseStatus(err))
	assert.False(t, d.Outdated())
}

func TestDriver_ImplementsStatusReporter(t *testing.T) {
	var d browser.Driver = NewDriver(testBrowser)
	_, ok := d.(browser.StatusReporter)
	assert.True(t, ok)
}

func TestDriver_CloseWindow(t *testing.T) {
	d, srv, rec := newDriver(t)
	var gotMethod string
	var got map[string]any
	startExt(t, srv, ProtocolVersion, func(method string, params json.RawMessage) (any, string) {
		gotMethod = method
		_ = json.Unmarshal(params, &got)
		return map[string]any{}, ""
	})
	waitConnected(t, rec)

	require.NoError(t, d.CloseWindow(context.Background(), "4"))
	assert.Equal(t, "windows.remove", gotMethod)
	assert.Equal(t, map[string]any{"windowId": float64(4)}, got)

	assert.Error(t, d.CloseWindow(context.Background(), "nope"))
}

func TestDriver_DropsEventsFromReplacedConnection(t *testing.T) {
	d, srv, rec, gate := newBlockedDriver(t)
	old := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	for i := 1; i <= 5; i++ {
		old.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": i}})
	}
	require.Eventually(t, func() bool { return len(d.events) == 4 }, 5*time.Second, 5*time.Millisecond)

	fresh := startExt(t, srv, ProtocolVersion, nil)
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.generation == 2
	}, 5*time.Second, 5*time.Millisecond)
	fresh.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": 100}})
	close(gate)

	require.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventWindowCreated, WindowID: "100"})
	}, 5*time.Second, 5*time.Millisecond)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	lastConnected := -1
	for i, ev := range rec.events {
		if ev.Type == browser.EventConnected {
			lastConnected = i
		}
	}
	require.Positive(t, lastConnected)
	assert.Equal(t, []browser.Event{
		{Type: browser.EventConnected},
		{Type: browser.EventWindowCreated, WindowID: "100"},
	}, rec.events[lastConnected:])
}

func TestDriver_ListWindows(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(method string, _ json.RawMessage) (any, string) {
		assert.Equal(t, "windows.list", method)
		return map[string]any{"windows": []wireWindow{{ID: 4, Focused: true, Tabs: []wireTab{{ID: 9, URL: "https://x", Active: true}}}}}, ""
	})
	waitConnected(t, rec)

	windows, err := d.ListWindows(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []browser.Window{{ID: "4", Focused: true, Tabs: []browser.Tab{{ID: "9", URL: "https://x", Active: true}}}}, windows)
}

func TestDriver_SendsTypedParams(t *testing.T) {
	d, srv, rec := newDriver(t)
	var got map[string]any
	startExt(t, srv, ProtocolVersion, func(method string, params json.RawMessage) (any, string) {
		_ = json.Unmarshal(params, &got)
		return wireTab{ID: 2, URL: "https://y"}, ""
	})
	waitConnected(t, rec)

	tab, err := d.OpenTab(context.Background(), "4", "https://y")
	require.NoError(t, err)
	assert.Equal(t, "2", tab.ID)
	assert.Equal(t, float64(4), got["windowId"])
	assert.Equal(t, "https://y", got["url"])
}

func TestDriver_ErrorResponse(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) { return nil, "No window with id: 4." })
	waitConnected(t, rec)

	err := d.FocusWindow(context.Background(), "4")
	assert.ErrorContains(t, err, "No window with id")
}

func TestDriver_TranslatesEvents(t *testing.T) {
	_, srv, rec := newDriver(t)
	ext := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	ext.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": 5}})
	ext.send(t, map[string]any{"type": "event", "event": "window.focused", "data": map[string]any{"windowId": -1}})

	assert.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventWindowCreated, WindowID: "5"}) &&
			rec.has(browser.Event{Type: browser.EventFocusChanged, WindowID: ""})
	}, 5*time.Second, 5*time.Millisecond)
}

func TestDriver_DisconnectFailsPendingAndEmits(t *testing.T) {
	d, srv, rec := newDriver(t)
	var ext *fakeExt
	ext = startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) {
		_ = ext.ws.Close(websocket.StatusGoingAway, "bye")
		return nil, ""
	})
	waitConnected(t, rec)

	_, err := d.ListWindows(context.Background())
	assert.ErrorIs(t, err, browser.ErrNotConnected)
	assert.Eventually(t, func() bool { return rec.has(browser.Event{Type: browser.EventDisconnected}) }, 5*time.Second, 5*time.Millisecond)
}

func TestDriver_NewConnectionReplacesOld(t *testing.T) {
	d, srv, rec := newDriver(t)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) {
		return map[string]any{"windows": []wireWindow{{ID: 1}}}, ""
	})
	waitConnected(t, rec)
	startExt(t, srv, ProtocolVersion, func(string, json.RawMessage) (any, string) {
		return map[string]any{"windows": []wireWindow{{ID: 2}}}, ""
	})

	assert.Eventually(t, func() bool {
		w, err := d.ListWindows(context.Background())
		return err == nil && len(w) == 1 && w[0].ID == "2"
	}, 5*time.Second, 10*time.Millisecond)
}

func TestDriver_DisconnectSurvivesFullBuffer(t *testing.T) {
	d, srv, rec, gate := newBlockedDriver(t)
	ext := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	fillBuffer(t, d, ext)
	require.NoError(t, ext.ws.Close(websocket.StatusGoingAway, "bye"))
	waitDropped(t, d)
	close(gate)

	assert.Eventually(t, func() bool { return rec.has(browser.Event{Type: browser.EventDisconnected}) }, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []browser.EventType{browser.EventConnected, browser.EventDisconnected}, rec.connectionEvents())
}

func TestDriver_ReconnectWhileBlockedEndsConnected(t *testing.T) {
	d, srv, rec, gate := newBlockedDriver(t)
	ext := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	fillBuffer(t, d, ext)
	require.NoError(t, ext.ws.Close(websocket.StatusGoingAway, "bye"))
	waitDropped(t, d)
	startExt(t, srv, ProtocolVersion, nil)
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.conn != nil
	}, 5*time.Second, 5*time.Millisecond)
	close(gate)

	assert.Eventually(t, func() bool {
		events := rec.connectionEvents()
		return len(events) >= 2 && events[len(events)-1] == browser.EventConnected
	}, 5*time.Second, 5*time.Millisecond)
	assert.NotContains(t, rec.connectionEvents()[1:], browser.EventDisconnected)
}

func TestDriver_SubscribeAfterConnectEmitsConnected(t *testing.T) {
	d := NewDriver(testBrowser)
	srv := httptest.NewServer(d.Handler())
	t.Cleanup(srv.Close)
	startExt(t, srv, ProtocolVersion, nil)
	require.Eventually(t, func() bool {
		d.mu.Lock()
		defer d.mu.Unlock()
		return d.conn != nil
	}, 5*time.Second, 5*time.Millisecond)

	rec := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = d.Subscribe(ctx, rec.add) }()
	waitConnected(t, rec)
}

func TestTranslateEvent(t *testing.T) {
	raw := func(s string) json.RawMessage { return json.RawMessage(s) }
	tests := []struct {
		in   message
		want browser.Event
		ok   bool
	}{
		{message{Event: "window.removed", Data: raw(`{"windowId":3}`)}, browser.Event{Type: browser.EventWindowRemoved, WindowID: "3"}, true},
		{message{Event: "tabs.changed", Data: raw(`{"windowId":3}`)}, browser.Event{Type: browser.EventTabsChanged, WindowID: "3"}, true},
		{message{Event: "window.created", Data: raw(`{}`)}, browser.Event{}, false},
		{message{Event: "nope", Data: raw(`{"windowId":3}`)}, browser.Event{}, false},
	}
	for _, tt := range tests {
		got, ok := translateEvent(tt.in)
		assert.Equal(t, tt.ok, ok, tt.in.Event)
		if tt.ok {
			assert.Equal(t, tt.want, got)
		}
	}
}

func TestDriver_ConnectedCarriesRunFromHello(t *testing.T) {
	_, srv, rec := newDriver(t)
	ws, err := dial(t, srv, "chrome-extension://abc")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.CloseNow() })
	ext := &fakeExt{ws: ws}
	ext.send(t, map[string]any{"type": "hello", "protocol": ProtocolVersion, "browser": "chrome", "run": "run-1"})
	go ext.serve()

	require.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventConnected, Run: "run-1"})
	}, 5*time.Second, 5*time.Millisecond)
}

func TestWindowTitle(t *testing.T) {
	cases := map[string]struct {
		window browser.Window
		want   string
	}{
		"active tab": {
			window: browser.Window{ID: "1", Tabs: []browser.Tab{
				{ID: "1", Title: "Other"},
				{ID: "2", Title: "Extensions - utena", Active: true},
			}},
			want: "Extensions - utena - Google Chrome",
		},
		"no active tab": {
			window: browser.Window{ID: "1", Tabs: []browser.Tab{{ID: "1", Title: "Other"}}},
			want:   "",
		},
		"no tabs": {
			window: browser.Window{ID: "1"},
			want:   "",
		},
		"empty title": {
			window: browser.Window{ID: "1", Tabs: []browser.Tab{{ID: "1", Active: true}}},
			want:   "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, NewDriver(testBrowser).WindowTitle(tc.window))
		})
	}
}

func (r *recorder) count(want browser.Event) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, ev := range r.events {
		if ev == want {
			n++
		}
	}
	return n
}

func TestDriver_CoalescesQueuedTabsChanged(t *testing.T) {
	d, srv, rec, gate := newBlockedDriver(t)
	ext := startExt(t, srv, ProtocolVersion, nil)
	waitConnected(t, rec)

	ext.send(t, map[string]any{"type": "event", "event": "window.created", "data": map[string]any{"windowId": 1}})
	require.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventWindowCreated, WindowID: "1"})
	}, 5*time.Second, 5*time.Millisecond)
	for range 3 {
		ext.send(t, map[string]any{"type": "event", "event": "tabs.changed", "data": map[string]any{"windowId": 7}})
	}
	ext.send(t, map[string]any{"type": "event", "event": "tabs.changed", "data": map[string]any{"windowId": 8}})
	ext.send(t, map[string]any{"type": "event", "event": "window.removed", "data": map[string]any{"windowId": 9}})
	require.Eventually(t, func() bool { return len(d.events) == 3 }, 5*time.Second, 5*time.Millisecond)
	close(gate)

	seven := browser.Event{Type: browser.EventTabsChanged, WindowID: "7"}
	require.Eventually(t, func() bool {
		return rec.has(browser.Event{Type: browser.EventWindowRemoved, WindowID: "9"})
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, rec.count(seven))
	assert.Equal(t, 1, rec.count(browser.Event{Type: browser.EventTabsChanged, WindowID: "8"}))

	ext.send(t, map[string]any{"type": "event", "event": "tabs.changed", "data": map[string]any{"windowId": 7}})
	assert.Eventually(t, func() bool { return rec.count(seven) == 2 }, 5*time.Second, 5*time.Millisecond)
}

func TestDriver_CreateWindowOmitsEmptyURLs(t *testing.T) {
	d, srv, rec := newDriver(t)
	var got []map[string]any
	startExt(t, srv, ProtocolVersion, func(method string, params json.RawMessage) (any, string) {
		var p map[string]any
		_ = json.Unmarshal(params, &p)
		got = append(got, p)
		return wireWindow{ID: 5}, ""
	})
	waitConnected(t, rec)

	_, err := d.CreateWindow(context.Background(), nil, false)
	require.NoError(t, err)
	_, err = d.CreateWindow(context.Background(), []string{"https://x"}, true)
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, map[string]any{"focused": false}, got[0])
	assert.Equal(t, map[string]any{"focused": true, "urls": []any{"https://x"}}, got[1])
}
