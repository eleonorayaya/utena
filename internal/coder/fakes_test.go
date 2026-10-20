package coder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type fakeProcess struct {
	once      sync.Once
	done      chan struct{}
	mu        sync.Mutex
	err       error
	wasKilled bool
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan struct{})}
}

func (p *fakeProcess) Exit(err error) {
	p.once.Do(func() {
		p.mu.Lock()
		p.err = err
		p.mu.Unlock()
		close(p.done)
	})
}

func (p *fakeProcess) Wait() error {
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.wasKilled = true
	p.mu.Unlock()
	p.Exit(errors.New("killed"))
	return nil
}

func (p *fakeProcess) killed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.wasKilled
}

type fakeRunner struct {
	mu       sync.Mutex
	runs     []Command
	starts   []Command
	procs    []*fakeProcess
	handle   func(Command) ([]byte, error)
	onStart  func(n int, p *fakeProcess)
	startErr error
}

func (r *fakeRunner) Run(_ context.Context, cmd Command) ([]byte, error) {
	r.mu.Lock()
	r.runs = append(r.runs, cmd)
	handle := r.handle
	r.mu.Unlock()
	if handle != nil {
		return handle(cmd)
	}
	return nil, nil
}

func (r *fakeRunner) Start(cmd Command) (Process, error) {
	r.mu.Lock()
	if r.startErr != nil {
		r.mu.Unlock()
		return nil, r.startErr
	}
	p := newFakeProcess()
	r.starts = append(r.starts, cmd)
	r.procs = append(r.procs, p)
	n := len(r.procs)
	onStart := r.onStart
	r.mu.Unlock()
	if onStart != nil {
		onStart(n, p)
	}
	return p, nil
}

func (r *fakeRunner) Runs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.runs))
	for i, c := range r.runs {
		out[i] = c.String()
	}
	return out
}

func (r *fakeRunner) RunCommands() []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Command(nil), r.runs...)
}

func (r *fakeRunner) Starts() []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Command(nil), r.starts...)
}

func (r *fakeRunner) Proc(i int) *fakeProcess {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.procs[i]
}

func (r *fakeRunner) StartCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.starts)
}

type fakeSessions struct {
	mu       sync.Mutex
	sessions map[uint]*session.Session
	remote   map[uint]uint
	states   map[uint][]session.RemoteState
}

func newFakeSessions(list ...session.Session) *fakeSessions {
	f := &fakeSessions{sessions: map[uint]*session.Session{}, remote: map[uint]uint{}, states: map[uint][]session.RemoteState{}}
	for i := range list {
		s := list[i]
		f.sessions[s.ID] = &s
	}
	return f
}

func (f *fakeSessions) CoderSessions() ([]session.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []session.Session
	for _, s := range f.sessions {
		out = append(out, *s)
	}
	return out, nil
}

func (f *fakeSessions) SetRemoteSessionID(id, remoteID uint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.remote[id] = remoteID
	return nil
}

func (f *fakeSessions) ApplyRemoteState(id uint, st session.RemoteState) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states[id] = append(f.states[id], st)
	return nil
}

func (f *fakeSessions) setAttached(id uint, attached bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessions[id].IsAttached = attached
}

func (f *fakeSessions) lastState(id uint) (session.RemoteState, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.states[id]
	if len(st) == 0 {
		return session.RemoteState{}, false
	}
	return st[len(st)-1], true
}

type followerCall struct {
	Method string
	Path   string
	Body   string
}

type fakeFollower struct {
	t         *testing.T
	srv       *httptest.Server
	mu        sync.Mutex
	calls     []followerCall
	statuses  []session.SessionStatus
	statusErr string
	attention claude.ClaudeSessionStatus
	prs       []git.PullRequest
	sessionID uint
	wsID      uint
}

func newFakeFollower(t *testing.T) *fakeFollower {
	f := &fakeFollower{t: t, sessionID: 42, wsID: 7, statuses: []session.SessionStatus{session.StatusActive}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeFollower) port() int {
	u, err := url.Parse(f.srv.URL)
	require.NoError(f.t, err)
	p, err := strconv.Atoi(u.Port())
	require.NoError(f.t, err)
	return p
}

func (f *fakeFollower) set(fn func(f *fakeFollower)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeFollower) Calls() []followerCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]followerCall(nil), f.calls...)
}

func (f *fakeFollower) CallsTo(method, prefix string) []followerCall {
	var out []followerCall
	for _, c := range f.Calls() {
		if c.Method == method && strings.HasPrefix(c.Path, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeFollower) serve(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	body := string(data)
	f.mu.Lock()
	path := r.URL.Path
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	if r.URL.Path != "/bundles" {
		f.calls = append(f.calls, followerCall{Method: r.Method, Path: path, Body: body})
	}
	sessPath := fmt.Sprintf("/sessions/%d", f.sessionID)
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/bundles":
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"bundles":[]}`))
	case r.Method == http.MethodPost && r.URL.Path == "/workspaces":
		var req workspace.AddWorkspaceRequest
		_ = json.Unmarshal([]byte(body), &req)
		ws := workspace.Workspace{Model: gorm.Model{ID: f.wsID}, Path: req.Path, LeaderWorkspaceID: req.LeaderWorkspaceID}
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(workspace.NewWorkspaceListResponse([]workspace.Workspace{{Model: gorm.Model{ID: 99}, Path: "/elsewhere"}, ws}))
	case r.Method == http.MethodPost && r.URL.Path == "/sessions":
		var req session.CreateSessionRequest
		_ = json.Unmarshal([]byte(body), &req)
		s := &session.Session{Model: gorm.Model{ID: f.sessionID}, Name: req.Name, Status: session.StatusCreating}
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(session.NewSessionResponse(s))
	case r.Method == http.MethodGet && r.URL.Path == sessPath:
		status := f.statuses[0]
		if len(f.statuses) > 1 {
			f.statuses = f.statuses[1:]
		}
		s := &session.Session{Model: gorm.Model{ID: f.sessionID}, Name: "ws", Status: status, StatusError: f.statusErr, PullRequests: f.prs}
		if f.attention != "" {
			s.ClaudeSessions = []claude.ClaudeSession{{ClaudeSessionID: "c1", Status: f.attention}}
		}
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(session.NewSessionResponse(s))
	case r.Method == http.MethodPut && (r.URL.Path == sessPath+"/activate" || r.URL.Path == sessPath+"/archive"):
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	case r.Method == http.MethodDelete && r.URL.Path == sessPath:
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		f.mu.Unlock()
		http.NotFound(w, r)
	}
}

func deadPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type recordingSleeper struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (s *recordingSleeper) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.waits = append(s.waits, d)
	s.mu.Unlock()
	return ctx.Err()
}

func (s *recordingSleeper) Waits() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.waits...)
}

func testConfig() Config {
	cfg, err := ParseConfig(FileConfig{Template: "my-template", Parameters: map[string]string{"size": "large"}}, 3333)
	if err != nil {
		panic(err)
	}
	return cfg
}

type harness struct {
	runner   *fakeRunner
	follower *fakeFollower
	sessions *fakeSessions
	clock    *manualClock
	sleeper  *recordingSleeper
	conns    *ConnManager
	svc      *Service
}

func newHarness(t *testing.T, sessions ...session.Session) *harness {
	t.Helper()
	h := &harness{
		runner:   &fakeRunner{},
		follower: newFakeFollower(t),
		sessions: newFakeSessions(sessions...),
		clock:    &manualClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		sleeper:  &recordingSleeper{},
	}
	port := h.follower.port()
	h.conns = NewConnManager(testConfig(), h.runner,
		WithPorts(func() (int, error) { return port, nil }),
		WithClock(h.clock.Now),
		WithSleeper(h.sleeper.Sleep),
	)
	h.svc = NewService(testConfig(), h.runner, h.conns, h.sessions)
	h.svc.now = h.clock.Now
	h.svc.sleep = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
			return nil
		}
	}
	t.Cleanup(h.conns.Close)
	return h
}
