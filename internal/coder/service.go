package coder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/session"
)

const (
	DefaultPollInterval = 3 * time.Second
	agentWaitTimeout    = 15 * time.Minute
	bestEffortTimeout   = 30 * time.Second
	FollowerHome        = "/home/coder"
	tokenFile           = "~/.config/utena/coder-keepalive-token"
	tokenLifetime       = "168h"
)

var ErrNoRemoteSession = common.NewInvalidRequest("coder session has no follower session")

type Sessions interface {
	CoderSessions() ([]session.Session, error)
	SetRemoteSessionID(id, remoteID uint) error
	ApplyRemoteState(id uint, st session.RemoteState) error
}

type Service struct {
	cfg          Config
	runner       Runner
	conns        *ConnManager
	sessions     Sessions
	now          Clock
	sleep        Sleeper
	pollInterval time.Duration

	locks  sync.Map
	busyMu sync.Mutex
	busy   map[string]bool
	cancel context.CancelFunc
	done   chan struct{}
}

func NewService(cfg Config, runner Runner, conns *ConnManager, sessions Sessions) *Service {
	return &Service{
		cfg:          cfg,
		runner:       runner,
		conns:        conns,
		sessions:     sessions,
		now:          time.Now,
		sleep:        sleepCtx,
		pollInterval: DefaultPollInterval,
		busy:         make(map[string]bool),
	}
}

func (s *Service) lock(workspace string) func() {
	actual, _ := s.locks.LoadOrStore(workspace, &sync.Mutex{})
	mu := actual.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Service) Enabled() bool {
	return s.cfg.Enabled()
}

func (s *Service) Config() Config {
	return s.cfg
}

func (s *Service) Connections() []string {
	out := s.conns.Workspaces()
	sort.Strings(out)
	return out
}

func (s *Service) WorkspaceName(name string, taken []string) (string, error) {
	return Slug(name, taken)
}

func (s *Service) coder(ctx context.Context, args ...string) ([]byte, error) {
	return s.runner.Run(ctx, Command{Name: "coder", Args: args})
}

func (s *Service) CreateArgs(workspace, repo string) []string {
	args := []string{"create", workspace, "-t", s.cfg.Template, "--parameter", "repo=" + repo}
	keys := make([]string, 0, len(s.cfg.Parameters))
	for k := range s.cfg.Parameters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--parameter", k+"="+s.cfg.Parameters[k])
	}
	return append(args, "--use-parameter-defaults", "-y")
}

func (s *Service) waitForAgent(ctx context.Context, workspace string) error {
	ctx, cancel := context.WithTimeout(ctx, agentWaitTimeout)
	defer cancel()
	if _, err := s.coder(ctx, "ssh", workspace, "--", "true"); err != nil {
		return fmt.Errorf("wait for workspace %s: %w", workspace, err)
	}
	return nil
}

type listedWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var errWorkspaceNotFound = errors.New("coder workspace not found")

func (s *Service) workspaceID(ctx context.Context, workspace string) (string, error) {
	out, err := s.coder(ctx, "list", "--search", "owner:me name:"+workspace, "-o", "json")
	if err != nil {
		return "", err
	}
	var rows []listedWorkspace
	if err := json.Unmarshal(out, &rows); err != nil {
		return "", fmt.Errorf("parse coder list output: %w", err)
	}
	for _, row := range rows {
		if row.Name == workspace && row.ID != "" {
			return row.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s", errWorkspaceNotFound, workspace)
}

func tokenName(workspace string) string {
	return "utena-" + workspace
}

func (s *Service) installKeepaliveToken(ctx context.Context, workspace string) error {
	id, err := s.workspaceID(ctx, workspace)
	if err != nil {
		return fmt.Errorf("look up workspace id: %w", err)
	}
	out, err := s.coder(ctx, "tokens", "create",
		"-n", tokenName(workspace),
		"--scope", "workspace:read",
		"--scope", "workspace:update",
		"--allow", "workspace:"+id,
		"--lifetime", tokenLifetime)
	if err != nil {
		return fmt.Errorf("create keepalive token: %w", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" || strings.ContainsAny(token, " \n") {
		return errors.New("create keepalive token: unexpected output")
	}
	script := "umask 077; mkdir -p ~/.config/utena; cat > " + tokenFile
	if _, err := s.runner.Run(ctx, Command{Name: "ssh", Args: []string{s.cfg.SSHHost(workspace), script}, Stdin: []byte(token + "\n")}); err != nil {
		return fmt.Errorf("write keepalive token: %w", err)
	}
	return nil
}

func repoName(repo string) (string, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return "", fmt.Errorf("invalid repo %q", repo)
	}
	return name, nil
}

func (s *Service) Create(ctx context.Context, in session.RemoteCreate) error {
	if !s.Enabled() {
		return errors.New("coder backend is not configured")
	}
	name, err := repoName(in.Repo)
	if err != nil {
		return err
	}
	defer s.lock(in.Workspace)()
	if _, err := s.coder(ctx, s.CreateArgs(in.Workspace, in.Repo)...); err != nil {
		return fmt.Errorf("coder create: %w", err)
	}
	if err := s.waitForAgent(ctx, in.Workspace); err != nil {
		return err
	}
	if err := s.installKeepaliveToken(ctx, in.Workspace); err != nil {
		return err
	}
	follower, err := s.conns.Ensure(ctx, in.Workspace)
	if err != nil {
		return err
	}
	followerWS, err := follower.LinkWorkspace(ctx, FollowerHome+"/"+name, in.LeaderWorkspaceID)
	if err != nil {
		return fmt.Errorf("register workspace on follower: %w", err)
	}
	leaderID := in.SessionID
	created, err := follower.CreateSession(ctx, session.CreateSessionRequest{
		Name: in.Workspace,
		Workspaces: []session.WorkspaceBranchSpec{{
			WorkspaceID: followerWS,
			Branch:      in.Spec.Branch,
			BaseBranch:  in.Spec.BaseBranch,
		}},
		Bundles:         in.Bundles,
		LeaderSessionID: &leaderID,
	})
	if err != nil {
		return fmt.Errorf("create follower session: %w", err)
	}
	if err := s.sessions.SetRemoteSessionID(in.SessionID, created.ID); err != nil {
		return fmt.Errorf("record follower session: %w", err)
	}
	return s.waitFollowerActive(ctx, in.Workspace, in.SessionID, created.ID)
}

func (s *Service) waitFollowerActive(ctx context.Context, workspace string, leaderID, remoteID uint) error {
	for {
		follower, err := s.conns.Ensure(ctx, workspace)
		if err != nil {
			return err
		}
		fs, err := follower.GetSession(ctx, remoteID)
		if err == nil {
			switch fs.Status {
			case session.StatusActive:
				s.apply(leaderID, workspace, fs)
				return nil
			case session.StatusBroken:
				msg := fs.StatusError
				if msg == "" {
					msg = "session setup failed"
				}
				return fmt.Errorf("follower: %s", msg)
			}
		} else if isNotFound(err) {
			return fmt.Errorf("follower session %d disappeared", remoteID)
		} else {
			slog.Debug("coder: follower poll failed", "workspace", workspace, "error", err)
		}
		if err := s.sleep(ctx, s.pollInterval); err != nil {
			return fmt.Errorf("wait for follower session: %w", err)
		}
	}
}

func (s *Service) start(ctx context.Context, workspace string) error {
	if _, err := s.coder(ctx, "start", workspace, "-y"); err != nil {
		return fmt.Errorf("coder start: %w", err)
	}
	return s.waitForAgent(ctx, workspace)
}

func (s *Service) Activate(ctx context.Context, sess *session.Session, resume bool) (string, error) {
	if sess.RemoteSessionID == nil {
		return "", ErrNoRemoteSession
	}
	ws := sess.CoderWorkspace
	defer s.lock(ws)()
	if resume {
		if err := s.start(ctx, ws); err != nil {
			return "", err
		}
	}
	follower, err := s.conns.Ensure(ctx, ws)
	if err != nil && !resume {
		slog.Info("coder: connection failed; starting workspace", "workspace", ws, "error", err)
		if startErr := s.start(ctx, ws); startErr != nil {
			return "", errors.Join(err, startErr)
		}
		follower, err = s.conns.Ensure(ctx, ws)
	}
	if err != nil {
		return "", err
	}
	if err := follower.ActivateSession(ctx, *sess.RemoteSessionID); err != nil {
		return "", fmt.Errorf("activate follower session: %w", err)
	}
	if fs, err := follower.GetSession(ctx, *sess.RemoteSessionID); err == nil {
		s.apply(sess.ID, ws, fs)
	}
	return ws, nil
}

func (s *Service) Archive(ctx context.Context, sess *session.Session) error {
	ws := sess.CoderWorkspace
	defer s.lock(ws)()
	if sess.RemoteSessionID != nil {
		bctx, cancel := context.WithTimeout(ctx, bestEffortTimeout)
		follower, err := s.conns.Ensure(bctx, ws)
		if err == nil {
			err = follower.ArchiveSession(bctx, *sess.RemoteSessionID)
		}
		cancel()
		if err != nil {
			slog.Warn("coder: archive on follower failed", "workspace", ws, "error", err)
		}
	}
	s.conns.Release(ws)
	s.setBusy(ws, false)
	if _, err := s.coder(ctx, "stop", ws, "-y"); err != nil {
		return fmt.Errorf("coder stop: %w", err)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, sess *session.Session) error {
	ws := sess.CoderWorkspace
	if ws == "" {
		return nil
	}
	defer s.lock(ws)()
	if sess.RemoteSessionID != nil {
		if follower, ok := s.conns.Follower(ws); ok {
			bctx, cancel := context.WithTimeout(ctx, bestEffortTimeout)
			if err := follower.DeleteSession(bctx, *sess.RemoteSessionID); err != nil {
				slog.Warn("coder: delete on follower failed", "workspace", ws, "error", err)
			}
			cancel()
		}
	}
	s.conns.Release(ws)
	s.setBusy(ws, false)
	bctx, cancel := context.WithTimeout(ctx, bestEffortTimeout)
	if _, err := s.coder(bctx, "tokens", "remove", tokenName(ws)); err != nil {
		slog.Warn("coder: delete keepalive token failed", "workspace", ws, "error", err)
	}
	cancel()
	if _, err := s.coder(ctx, "delete", ws, "-y"); err != nil {
		if _, lookupErr := s.workspaceID(ctx, ws); errors.Is(lookupErr, errWorkspaceNotFound) {
			return nil
		}
		return fmt.Errorf("coder delete: %w", err)
	}
	return nil
}

func (s *Service) Refresh(ctx context.Context, id uint) error {
	sess, err := s.findSession(id)
	if err != nil {
		return err
	}
	if sess.RemoteSessionID == nil {
		return ErrNoRemoteSession
	}
	follower, err := s.conns.Ensure(ctx, sess.CoderWorkspace)
	if err != nil {
		return err
	}
	fs, err := follower.GetSession(ctx, *sess.RemoteSessionID)
	if err != nil {
		return err
	}
	s.apply(sess.ID, sess.CoderWorkspace, fs)
	return nil
}

var errSessionNotCoder = common.NewNotFound("coder session not found")

func (s *Service) findSession(id uint) (*session.Session, error) {
	sessions, err := s.sessions.CoderSessions()
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].ID == id {
			return &sessions[i], nil
		}
	}
	return nil, errSessionNotCoder
}

func isBusy(st claude.ClaudeSessionStatus) bool {
	return st == claude.StatusWorking || st == claude.StatusNeedsAttention
}

func (s *Service) setBusy(workspace string, busy bool) {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	if busy {
		s.busy[workspace] = true
	} else {
		delete(s.busy, workspace)
	}
}

func (s *Service) isBusy(workspace string) bool {
	s.busyMu.Lock()
	defer s.busyMu.Unlock()
	return s.busy[workspace]
}

func (s *Service) apply(leaderID uint, workspace string, fs *FollowerSession) {
	attention := fs.AttentionStatus
	if attention == "" {
		attention = claude.AggregateStatus(fs.ClaudeSessions)
	}
	s.setBusy(workspace, isBusy(attention))
	st := session.RemoteState{
		Status:       fs.Status,
		StatusError:  fs.StatusError,
		Attention:    attention,
		PullRequests: fs.PullRequests,
		SyncedAt:     s.now(),
	}
	if err := s.sessions.ApplyRemoteState(leaderID, st); err != nil {
		slog.Warn("coder: apply follower state failed", "session", leaderID, "error", err)
	}
}

var unpolledStatuses = []session.SessionStatus{session.StatusArchived, session.StatusDeleted, session.StatusCreating}

func (s *Service) poll(ctx context.Context) {
	sessions, err := s.sessions.CoderSessions()
	if err != nil {
		slog.Warn("coder: list sessions failed", "error", err)
		return
	}
	attached := make(map[string]bool)
	for i := range sessions {
		sess := &sessions[i]
		if sess.IsAttached {
			attached[sess.CoderWorkspace] = true
		}
		if sess.RemoteSessionID == nil || slices.Contains(unpolledStatuses, sess.Status) {
			continue
		}
		follower, ok := s.conns.Follower(sess.CoderWorkspace)
		if !ok {
			continue
		}
		pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		fs, err := follower.GetSession(pctx, *sess.RemoteSessionID)
		cancel()
		if err != nil {
			slog.Debug("coder: follower poll failed", "workspace", sess.CoderWorkspace, "error", err)
			continue
		}
		s.apply(sess.ID, sess.CoderWorkspace, fs)
	}
	s.conns.Tick(func(ws string) bool {
		return attached[ws] || s.isBusy(ws)
	})
}

func (s *Service) Start(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	go s.restoreConnections(context.WithoutCancel(ctx))
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.cancel = cancel
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.poll(ctx)
			}
		}
	}()
}

func (s *Service) restoreConnections(ctx context.Context) {
	sessions, err := s.sessions.CoderSessions()
	if err != nil {
		slog.Warn("coder: list sessions failed", "error", err)
		return
	}
	for i := range sessions {
		sess := sessions[i]
		if sess.CoderWorkspace == "" || sess.Status == session.StatusCreating || sess.Status == session.StatusDeleted {
			continue
		}
		if sess.IsAttached && sess.Status != session.StatusArchived {
			go func() {
				ectx, cancel := context.WithTimeout(ctx, readyTimeout)
				defer cancel()
				if _, err := s.conns.Ensure(ectx, sess.CoderWorkspace); err != nil {
					slog.Warn("coder: reconnect attached session failed", "workspace", sess.CoderWorkspace, "error", err)
				}
			}()
			continue
		}
		s.conns.removeHost(sess.CoderWorkspace)
	}
}

func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	s.conns.Close()
}
