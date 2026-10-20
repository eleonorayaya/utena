package coder

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	readyTimeout      = 90 * time.Second
	readyPollInterval = 250 * time.Millisecond
	minRestartBackoff = time.Second
	maxRestartBackoff = 30 * time.Second
	hostCmdTimeout    = 30 * time.Second
	connectAttempts   = 3
)

type PortAllocator func() (int, error)

func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

type Clock func() time.Time

type Sleeper func(ctx context.Context, d time.Duration) error

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type conn struct {
	workspace  string
	port       int
	proc       Process
	up         bool
	lastNeeded time.Time
	generation int
}

type ConnManager struct {
	cfg        Config
	runner     Runner
	ports      PortAllocator
	now        Clock
	sleep      Sleeper
	httpClient *http.Client
	ctx        context.Context
	cancel     context.CancelFunc

	locks sync.Map
	mu    sync.Mutex
	conns map[string]*conn
	wg    sync.WaitGroup
}

type ConnOption func(*ConnManager)

func WithPorts(p PortAllocator) ConnOption { return func(m *ConnManager) { m.ports = p } }
func WithClock(c Clock) ConnOption         { return func(m *ConnManager) { m.now = c } }
func WithSleeper(s Sleeper) ConnOption     { return func(m *ConnManager) { m.sleep = s } }
func WithHTTPClient(c *http.Client) ConnOption {
	return func(m *ConnManager) { m.httpClient = c }
}

func NewConnManager(cfg Config, runner Runner, opts ...ConnOption) *ConnManager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &ConnManager{
		cfg:        cfg,
		runner:     runner,
		ports:      FreePort,
		now:        time.Now,
		sleep:      sleepCtx,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		ctx:        ctx,
		cancel:     cancel,
		conns:      make(map[string]*conn),
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *ConnManager) lock(workspace string) func() {
	actual, _ := m.locks.LoadOrStore(workspace, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (m *ConnManager) baseURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

func (m *ConnManager) Connected(workspace string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[workspace]
	if !ok || !c.up {
		return "", false
	}
	return m.baseURL(c.port), true
}

func (m *ConnManager) Follower(workspace string) (*FollowerClient, bool) {
	url, ok := m.Connected(workspace)
	if !ok {
		return nil, false
	}
	return NewFollowerClient(url, m.httpClient), true
}

func (m *ConnManager) Ensure(ctx context.Context, workspace string) (*FollowerClient, error) {
	return m.ensure(ctx, workspace, true)
}

var ErrClosed = errors.New("coder connection manager is closed")

func (m *ConnManager) ensure(ctx context.Context, workspace string, bump bool) (*FollowerClient, error) {
	defer m.lock(workspace)()
	if m.ctx.Err() != nil {
		return nil, ErrClosed
	}
	m.mu.Lock()
	c, ok := m.conns[workspace]
	if ok {
		if bump {
			c.lastNeeded = m.now()
		}
		if c.up {
			url := m.baseURL(c.port)
			m.mu.Unlock()
			return NewFollowerClient(url, m.httpClient), nil
		}
	}
	m.mu.Unlock()

	if !ok {
		if err := m.addHost(ctx, workspace); err != nil {
			return nil, err
		}
	}
	proc, port, err := m.connect(ctx, workspace)
	if err != nil {
		if !ok {
			m.removeHost(workspace)
		}
		return nil, err
	}

	if m.ctx.Err() != nil {
		_ = proc.Kill()
		m.removeHost(workspace)
		return nil, ErrClosed
	}
	m.mu.Lock()
	c, ok = m.conns[workspace]
	if !ok {
		c = &conn{workspace: workspace, lastNeeded: m.now()}
		m.conns[workspace] = c
	}
	c.port = port
	c.proc = proc
	c.up = true
	if bump {
		c.lastNeeded = m.now()
	}
	c.generation++
	gen := c.generation
	m.mu.Unlock()

	m.wg.Add(1)
	go m.supervise(workspace, proc, gen)
	return NewFollowerClient(m.baseURL(port), m.httpClient), nil
}

func (m *ConnManager) addHost(ctx context.Context, workspace string) error {
	hctx, cancel := context.WithTimeout(ctx, hostCmdTimeout)
	defer cancel()
	if _, err := m.runner.Run(hctx, Command{Name: "tuios", Args: []string{"hosts", "add", workspace, m.cfg.SSHHost(workspace)}}); err != nil {
		return fmt.Errorf("tuios hosts add %s: %w", workspace, err)
	}
	return nil
}

func (m *ConnManager) removeHost(workspace string) {
	ctx, cancel := context.WithTimeout(context.Background(), hostCmdTimeout)
	defer cancel()
	if _, err := m.runner.Run(ctx, Command{Name: "tuios", Args: []string{"hosts", "remove", workspace}}); err != nil {
		slog.Warn("coder: tuios hosts remove failed", "workspace", workspace, "error", err)
	}
}

func (m *ConnManager) SSHCommand(workspace string, localPort int) Command {
	return Command{Name: "ssh", Args: []string{
		"-N",
		"-o", "BatchMode=yes",
		"-o", "ExitOnForwardFailure=yes",
		"-o", "ServerAliveInterval=15",
		"-L", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", localPort, m.cfg.FollowerPort),
		"-R", fmt.Sprintf("127.0.0.1:%d:127.0.0.1:%d", m.cfg.LeaderPort, m.cfg.LocalAPIPort),
		m.cfg.SSHHost(workspace),
	}}
}

type watchedProcess struct {
	proc Process
	done chan struct{}
	err  error
}

func watch(proc Process) *watchedProcess {
	w := &watchedProcess{proc: proc, done: make(chan struct{})}
	go func() {
		w.err = proc.Wait()
		close(w.done)
	}()
	return w
}

func (w *watchedProcess) Wait() error {
	<-w.done
	return w.err
}

func (w *watchedProcess) Kill() error {
	return w.proc.Kill()
}

var errForwardExited = errors.New("ssh forward exited")

func (m *ConnManager) connect(ctx context.Context, workspace string) (*watchedProcess, int, error) {
	var lastErr error
	for attempt := 0; attempt < connectAttempts; attempt++ {
		port, err := m.ports()
		if err != nil {
			return nil, 0, fmt.Errorf("allocate local port: %w", err)
		}
		started, err := m.runner.Start(m.SSHCommand(workspace, port))
		if err != nil {
			return nil, 0, fmt.Errorf("start ssh forward to %s: %w", workspace, err)
		}
		proc := watch(started)
		err = m.waitReady(ctx, proc, port)
		if err == nil {
			return proc, port, nil
		}
		lastErr = err
		_ = proc.Kill()
		if ctx.Err() != nil || !errors.Is(err, errForwardExited) {
			break
		}
	}
	return nil, 0, fmt.Errorf("connect to %s: %w", workspace, lastErr)
}

func (m *ConnManager) waitReady(ctx context.Context, proc *watchedProcess, port int) error {
	ctx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()
	follower := NewFollowerClient(m.baseURL(port), m.httpClient)
	for {
		pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
		err := follower.Ping(pingCtx)
		pingCancel()
		select {
		case <-proc.done:
			return fmt.Errorf("%w: %v", errForwardExited, proc.err)
		default:
		}
		if err == nil {
			return nil
		}
		select {
		case <-proc.done:
			return fmt.Errorf("%w: %v", errForwardExited, proc.err)
		case <-ctx.Done():
			return fmt.Errorf("follower not reachable through the forward: %w", err)
		case <-time.After(readyPollInterval):
		}
	}
}

func (m *ConnManager) isCurrent(workspace string, gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[workspace]
	return ok && c.generation == gen
}

func (m *ConnManager) supervise(workspace string, proc *watchedProcess, gen int) {
	defer m.wg.Done()
	err := proc.Wait()
	m.mu.Lock()
	if c, ok := m.conns[workspace]; ok && c.generation == gen {
		c.up = false
	}
	m.mu.Unlock()
	backoff := minRestartBackoff
	for m.ctx.Err() == nil && m.isCurrent(workspace, gen) {
		slog.Warn("coder: ssh forward exited; restarting", "workspace", workspace, "error", err, "backoff", backoff)
		if m.sleep(m.ctx, backoff) != nil || !m.isCurrent(workspace, gen) {
			return
		}
		if _, err = m.ensure(m.ctx, workspace, false); err == nil {
			return
		}
		backoff = min(backoff*2, maxRestartBackoff)
	}
}

func (m *ConnManager) Release(workspace string) {
	defer m.lock(workspace)()
	m.mu.Lock()
	c, ok := m.conns[workspace]
	if ok {
		delete(m.conns, workspace)
	}
	m.mu.Unlock()
	if ok && c.proc != nil {
		_ = c.proc.Kill()
	}
	m.removeHost(workspace)
}

func (m *ConnManager) Workspaces() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.conns))
	for ws := range m.conns {
		out = append(out, ws)
	}
	return out
}

func (m *ConnManager) Tick(needed func(workspace string) bool) {
	now := m.now()
	var idle []string
	m.mu.Lock()
	snapshot := make([]*conn, 0, len(m.conns))
	for _, c := range m.conns {
		snapshot = append(snapshot, c)
	}
	m.mu.Unlock()
	for _, c := range snapshot {
		hold := needed(c.workspace)
		m.mu.Lock()
		if hold {
			c.lastNeeded = now
		} else if now.Sub(c.lastNeeded) >= m.cfg.IdleDisconnect {
			idle = append(idle, c.workspace)
		}
		m.mu.Unlock()
	}
	for _, ws := range idle {
		slog.Info("coder: dropping idle connection", "workspace", ws)
		m.Release(ws)
	}
}

func (m *ConnManager) Close() {
	m.cancel()
	for _, ws := range m.Workspaces() {
		m.Release(ws)
	}
	m.wg.Wait()
}
