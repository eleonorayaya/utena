package webext

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/eleonorayaya/utena/internal/browser"
)

type Browser struct {
	Name         string
	BundleID     string
	OriginScheme string
	UpdateHint   string
	TitleSuffix  string
}

type Driver struct {
	browser      Browser
	mu           sync.Mutex
	conn         *conn
	generation   uint64
	events       chan queuedEvent
	stateChanged chan struct{}
	nextID       atomic.Int64
	timeout      time.Duration
	outdated     atomic.Bool

	queuedMu   sync.Mutex
	queuedTabs map[queuedEvent]struct{}
}

var (
	_ browser.Driver         = (*Driver)(nil)
	_ browser.HTTPDriver     = (*Driver)(nil)
	_ browser.StatusReporter = (*Driver)(nil)
)

var errProtocolMismatch = errors.New("protocol mismatch")

type queuedEvent struct {
	gen uint64
	ev  browser.Event
}

type conn struct {
	ws      *websocket.Conn
	gen     uint64
	run     string
	mu      sync.Mutex
	pending map[int64]chan message
}

func NewDriver(b Browser) *Driver {
	return &Driver{
		browser:      b,
		events:       make(chan queuedEvent, 256),
		queuedTabs:   map[queuedEvent]struct{}{},
		stateChanged: make(chan struct{}, 1),
		timeout:      5 * time.Second,
	}
}

func (d *Driver) Name() string {
	return d.browser.Name
}

func (d *Driver) Outdated() bool {
	return d.outdated.Load()
}

func (d *Driver) UpdateHint() string {
	return d.browser.UpdateHint
}

func (d *Driver) AppBundleID() string {
	return d.browser.BundleID
}

func (d *Driver) WindowTitle(w browser.Window) string {
	t, ok := w.ActiveTab()
	if !ok || t.Title == "" {
		return ""
	}
	return t.Title + d.browser.TitleSuffix
}

func (d *Driver) Handler() http.Handler {
	return http.HandlerFunc(d.serve)
}

func (d *Driver) serve(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Origin"), d.browser.OriginScheme) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		slog.Warn("browser extension: accept websocket failed", "browser", d.browser.Name, "error", err)
		return
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(4 << 20)
	ctx := r.Context()

	run, err := readHello(ctx, ws, d.browser.Name)
	if err != nil {
		if errors.Is(err, errProtocolMismatch) {
			d.markOutdated()
		}
		slog.Warn("browser extension rejected", "browser", d.browser.Name, "error", err)
		_ = ws.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	d.outdated.Store(false)

	c := &conn{ws: ws, run: run, pending: make(map[int64]chan message)}
	d.swap(c)
	slog.Info("browser extension connected", "browser", d.browser.Name)
	err = d.readLoop(ctx, c)
	d.drop(c)
	slog.Info("browser extension disconnected", "browser", d.browser.Name, "reason", err)
}

func readHello(ctx context.Context, ws *websocket.Conn, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, data, err := ws.Read(ctx)
	if err != nil {
		return "", err
	}
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		return "", err
	}
	if m.Type != "hello" {
		return "", fmt.Errorf("expected hello, got %q", m.Type)
	}
	if m.Browser != name {
		return "", fmt.Errorf("extension is for %q, daemon drives %q", m.Browser, name)
	}
	if m.Protocol != ProtocolVersion {
		return "", fmt.Errorf("%w: extension protocol %d, daemon expects %d; reload the extension", errProtocolMismatch, m.Protocol, ProtocolVersion)
	}
	return m.Run, nil
}

func (d *Driver) markOutdated() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		d.outdated.Store(true)
	}
}

func (d *Driver) swap(c *conn) {
	d.mu.Lock()
	old := d.conn
	d.conn = c
	d.generation++
	c.gen = d.generation
	d.mu.Unlock()
	d.signalState()
	if old != nil {
		go func() { _ = old.ws.Close(websocket.StatusNormalClosure, "replaced") }()
	}
}

func (d *Driver) drop(c *conn) {
	d.mu.Lock()
	current := d.conn == c
	if current {
		d.conn = nil
	}
	d.mu.Unlock()
	c.failPending()
	if current {
		d.signalState()
	}
}

func (d *Driver) signalState() {
	select {
	case d.stateChanged <- struct{}{}:
	default:
	}
}

func (d *Driver) connectionState() (bool, uint64, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return false, d.generation, ""
	}
	return true, d.generation, d.conn.run
}

func (d *Driver) readLoop(ctx context.Context, c *conn) error {
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			return err
		}
		var m message
		if err := json.Unmarshal(data, &m); err != nil {
			slog.Warn("browser extension: bad frame", "error", err)
			continue
		}
		switch m.Type {
		case "res":
			c.resolve(m)
		case "event":
			if ev, ok := translateEvent(m); ok {
				d.emit(c.gen, ev)
			}
		}
	}
}

func (d *Driver) emit(gen uint64, ev browser.Event) {
	qe := queuedEvent{gen: gen, ev: ev}
	coalesce := ev.Type == browser.EventTabsChanged
	if coalesce {
		d.queuedMu.Lock()
		_, queued := d.queuedTabs[qe]
		d.queuedTabs[qe] = struct{}{}
		d.queuedMu.Unlock()
		if queued {
			return
		}
	}
	select {
	case d.events <- qe:
	default:
		if coalesce {
			d.dequeued(qe)
		}
		slog.Warn("browser extension: event buffer full; dropping", "type", ev.Type)
	}
}

func (d *Driver) dequeued(qe queuedEvent) {
	d.queuedMu.Lock()
	delete(d.queuedTabs, qe)
	d.queuedMu.Unlock()
}

func (c *conn) resolve(m message) {
	c.mu.Lock()
	ch := c.pending[m.ID]
	delete(c.pending, m.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (c *conn) failPending() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ch := range c.pending {
		close(ch)
	}
	c.pending = nil
}

func (d *Driver) call(ctx context.Context, method string, params, out any) error {
	d.mu.Lock()
	c := d.conn
	d.mu.Unlock()
	if c == nil {
		return browser.ErrNotConnected
	}

	id := d.nextID.Add(1)
	ch := make(chan message, 1)
	c.mu.Lock()
	if c.pending == nil {
		c.mu.Unlock()
		return browser.ErrNotConnected
	}
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	data, err := json.Marshal(message{Type: "req", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return errors.Join(browser.ErrNotConnected, err)
	}

	select {
	case res, ok := <-ch:
		if !ok {
			return browser.ErrNotConnected
		}
		if res.Error != "" {
			return fmt.Errorf("%s %s: %s", d.browser.Name, method, res.Error)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(res.Result, out)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func parseID(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid browser id %q", id)
	}
	return n, nil
}

func (d *Driver) ListWindows(ctx context.Context) ([]browser.Window, error) {
	var out struct {
		Windows []wireWindow `json:"windows"`
	}
	if err := d.call(ctx, "windows.list", nil, &out); err != nil {
		return nil, err
	}
	windows := make([]browser.Window, len(out.Windows))
	for i, w := range out.Windows {
		windows[i] = w.toWindow()
	}
	return windows, nil
}

func (d *Driver) CreateWindow(ctx context.Context, urls []string, focused bool) (browser.Window, error) {
	var out wireWindow
	if err := d.call(ctx, "windows.create", map[string]any{"urls": urls, "focused": focused}, &out); err != nil {
		return browser.Window{}, err
	}
	return out.toWindow(), nil
}

func (d *Driver) OpenTab(ctx context.Context, windowID, url string) (browser.Tab, error) {
	wid, err := parseID(windowID)
	if err != nil {
		return browser.Tab{}, err
	}
	var out wireTab
	if err := d.call(ctx, "tabs.create", map[string]any{"windowId": wid, "url": url}, &out); err != nil {
		return browser.Tab{}, err
	}
	return out.toTab(), nil
}

func (d *Driver) ActivateTab(ctx context.Context, windowID, tabID string) error {
	wid, err := parseID(windowID)
	if err != nil {
		return err
	}
	tid, err := parseID(tabID)
	if err != nil {
		return err
	}
	return d.call(ctx, "tabs.activate", map[string]any{"windowId": wid, "tabId": tid}, nil)
}

func (d *Driver) FocusWindow(ctx context.Context, windowID string) error {
	wid, err := parseID(windowID)
	if err != nil {
		return err
	}
	return d.call(ctx, "windows.focus", map[string]any{"windowId": wid}, nil)
}

func (d *Driver) CloseWindow(ctx context.Context, windowID string) error {
	wid, err := parseID(windowID)
	if err != nil {
		return err
	}
	return d.call(ctx, "windows.remove", map[string]any{"windowId": wid}, nil)
}

func (d *Driver) Subscribe(ctx context.Context, onEvent func(browser.Event)) error {
	var deliveredGen uint64
	deliveredConnected := false
	syncState := func() {
		connected, gen, run := d.connectionState()
		switch {
		case connected && gen != deliveredGen:
			deliveredGen, deliveredConnected = gen, true
			onEvent(browser.Event{Type: browser.EventConnected, Run: run})
		case !connected && deliveredConnected:
			deliveredConnected = false
			onEvent(browser.Event{Type: browser.EventDisconnected})
		}
	}

	syncState()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.stateChanged:
			syncState()
		case qe := <-d.events:
			if qe.ev.Type == browser.EventTabsChanged {
				d.dequeued(qe)
			}
			select {
			case <-d.stateChanged:
			default:
			}
			syncState()
			if deliveredConnected && qe.gen == deliveredGen {
				onEvent(qe.ev)
			}
		}
	}
}
