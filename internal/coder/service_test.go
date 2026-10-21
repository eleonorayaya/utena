package coder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testToken = "tok-very-secret"

func coderHandler(extra func(Command) ([]byte, error, bool)) func(Command) ([]byte, error) {
	return func(c Command) ([]byte, error) {
		if extra != nil {
			if out, err, ok := extra(c); ok {
				return out, err
			}
		}
		if c.Name != "coder" || len(c.Args) == 0 {
			return nil, nil
		}
		switch {
		case c.Args[0] == "list":
			return []byte(`[{"id":"other-id","name":"ws-old"},{"id":"ws-id","name":"ws"}]`), nil
		case c.Args[0] == "tokens" && c.Args[1] == "create":
			return []byte(testToken + "\n"), nil
		}
		return nil, nil
	}
}

func remoteID(id uint) *uint { return &id }

func coderSession(id uint, ws string, status session.SessionStatus, remote *uint) session.Session {
	return session.Session{Model: gorm.Model{ID: id}, Name: ws, Status: status, Backend: session.BackendCoder, CoderWorkspace: ws, RemoteSessionID: remote}
}

func TestService_CreateArgs(t *testing.T) {
	cfg, err := ParseConfig(FileConfig{Template: "my-template", Parameters: map[string]string{"zone": "b", "size": "large"}}, 3333)
	require.NoError(t, err)
	svc := NewService(cfg, &fakeRunner{}, nil, nil)
	assert.Equal(t, []string{
		"create", "ws", "-t", "my-template",
		"--parameter", "repo=acme/widgets",
		"--parameter", "size=large",
		"--parameter", "zone=b",
		"--use-parameter-defaults", "-y",
	}, svc.CreateArgs("ws", "acme/widgets"))
}

func TestService_CreateRunsLifecycleInOrder(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(nil)
	h.follower.set(func(f *fakeFollower) {
		f.statuses = []session.SessionStatus{session.StatusCreating, session.StatusCreating, session.StatusActive}
	})

	err := h.svc.Create(context.Background(), session.RemoteCreate{
		SessionID:         9,
		Workspace:         "ws",
		Repo:              "acme/widgets",
		LeaderWorkspaceID: 5,
		Spec:              session.WorkspaceBranchSpec{WorkspaceID: 5, BaseBranch: "main"},
	})
	require.NoError(t, err)

	assert.Equal(t, []string{
		"coder create ws -t my-template --parameter repo=acme/widgets --parameter size=large --use-parameter-defaults -y",
		"coder ssh ws -- true",
		"coder list --search owner:me name:ws -o json",
		"coder tokens create -n utena-ws --scope workspace:read --scope workspace:update --allow workspace:ws-id --lifetime 168h",
		"ssh ws.coder umask 077; mkdir -p ~/.config/utena; cat > ~/.config/utena/coder-keepalive-token",
		"tuios hosts add ws ws.coder",
	}, h.runner.Runs())

	runs := h.runner.RunCommands()
	assert.Nil(t, runs[0].Stdin, "coder create runs with stdin closed")
	assert.Equal(t, testToken+"\n", string(runs[4].Stdin))
	for _, c := range append(runs, h.runner.Starts()...) {
		for _, a := range c.Args {
			assert.NotContains(t, a, testToken)
		}
	}

	port := h.follower.port()
	require.Len(t, h.runner.Starts(), 1)
	assert.Equal(t, fmt.Sprintf("ssh -N -o BatchMode=yes -o ExitOnForwardFailure=yes -o ServerAliveInterval=15 -L 127.0.0.1:%d:127.0.0.1:3333 -R 127.0.0.1:3334:127.0.0.1:3333 ws.coder", port), h.runner.Starts()[0].String())

	wsCalls := h.follower.CallsTo("POST", "/workspaces")
	require.Len(t, wsCalls, 1)
	assert.JSONEq(t, `{"path":"/home/coder/widgets","as_root":false,"leader_workspace_id":5}`, wsCalls[0].Body)

	sessCalls := h.follower.CallsTo("POST", "/sessions")
	require.Len(t, sessCalls, 1)
	assert.JSONEq(t, `{"name":"ws","workspaces":[{"workspace_id":7,"base_branch":"main"}],"bundles":null,"leader_session_id":9}`, sessCalls[0].Body)

	assert.Equal(t, uint(42), h.sessions.remote[9])
	st, ok := h.sessions.lastState(9)
	require.True(t, ok)
	assert.Equal(t, session.StatusActive, st.Status)
	assert.Len(t, h.follower.CallsTo("GET", "/sessions/42"), 3)
	_, connected := h.conns.Connected("ws")
	assert.True(t, connected)
}

func TestService_CreatePassesBranchAndBundles(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(nil)

	err := h.svc.Create(context.Background(), session.RemoteCreate{
		SessionID: 3, Workspace: "ws", Repo: "acme/widgets", LeaderWorkspaceID: 5,
		Spec:    session.WorkspaceBranchSpec{WorkspaceID: 5, Branch: "feature/x"},
		Bundles: []string{"go"},
	})
	require.NoError(t, err)
	sessCalls := h.follower.CallsTo("POST", "/sessions")
	require.Len(t, sessCalls, 1)
	assert.JSONEq(t, `{"name":"ws","workspaces":[{"workspace_id":7,"branch":"feature/x"}],"bundles":["go"],"leader_session_id":3}`, sessCalls[0].Body)
}

func TestService_CreateFailsWhenFollowerSessionBreaks(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(nil)
	h.follower.set(func(f *fakeFollower) {
		f.statuses = []session.SessionStatus{session.StatusCreating, session.StatusBroken}
		f.statusErr = "worktree setup failed: boom"
	})

	err := h.svc.Create(context.Background(), session.RemoteCreate{SessionID: 9, Workspace: "ws", Repo: "acme/widgets", LeaderWorkspaceID: 5, Spec: session.WorkspaceBranchSpec{BaseBranch: "main"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "worktree setup failed: boom")
	assert.Equal(t, uint(42), h.sessions.remote[9], "follower id is recorded so delete can clean up")
}

func TestService_CreateStopsOnCoderCreateFailure(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(func(c Command) ([]byte, error, bool) {
		if c.Name == "coder" && c.Args[0] == "create" {
			return nil, errors.New("template not found"), true
		}
		return nil, nil, false
	})

	err := h.svc.Create(context.Background(), session.RemoteCreate{SessionID: 9, Workspace: "ws", Repo: "acme/widgets", Spec: session.WorkspaceBranchSpec{BaseBranch: "main"}})
	require.ErrorContains(t, err, "template not found")
	assert.Len(t, h.runner.Runs(), 1)
	assert.Empty(t, h.follower.Calls())
}

func TestService_CreateRejectsUnexpectedTokenOutput(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = coderHandler(func(c Command) ([]byte, error, bool) {
		if c.Name == "coder" && c.Args[0] == "tokens" {
			return []byte(""), nil, true
		}
		return nil, nil, false
	})
	err := h.svc.Create(context.Background(), session.RemoteCreate{SessionID: 9, Workspace: "ws", Repo: "acme/widgets", Spec: session.WorkspaceBranchSpec{BaseBranch: "main"}})
	require.ErrorContains(t, err, "keepalive token")
}

func TestService_WorkspaceIDNeedsExactName(t *testing.T) {
	h := newHarness(t)
	h.runner.handle = func(Command) ([]byte, error) {
		return []byte(`[{"id":"a","name":"ws-2"}]`), nil
	}
	_, err := h.svc.workspaceID(context.Background(), "ws")
	assert.ErrorIs(t, err, errWorkspaceNotFound)
}

func TestService_ActivateResumeStartsWorkspace(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusCreating, remoteID(42))
	h := newHarness(t, sess)
	h.runner.handle = coderHandler(nil)

	host, err := h.svc.Activate(context.Background(), &sess, true)
	require.NoError(t, err)
	assert.Equal(t, "ws", host)
	assert.Equal(t, []string{"coder start ws -y", "coder ssh ws -- true", "tuios hosts add ws ws.coder"}, h.runner.Runs())
	assert.Len(t, h.follower.CallsTo("PUT", "/sessions/42/activate"), 1)
}

func TestService_ActivateConnectedSkipsStart(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, remoteID(42))
	h := newHarness(t, sess)

	host, err := h.svc.Activate(context.Background(), &sess, false)
	require.NoError(t, err)
	assert.Equal(t, "ws", host)
	assert.Equal(t, []string{"tuios hosts add ws ws.coder"}, h.runner.Runs())
	assert.Len(t, h.follower.CallsTo("PUT", "/sessions/42/activate"), 1)
	_, err = h.svc.Activate(context.Background(), &sess, false)
	require.NoError(t, err)
	assert.Equal(t, 1, h.runner.StartCount(), "an up connection is reused")
}

func TestService_ActivateStartsStoppedWorkspaceWhenConnectFails(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, remoteID(42))
	h := newHarness(t, sess)
	dead, live := deadPort(t), h.follower.port()
	calls := 0
	h.conns.ports = func() (int, error) {
		calls++
		if calls <= connectAttempts {
			return dead, nil
		}
		return live, nil
	}
	h.runner.onStart = func(n int, p *fakeProcess) {
		if n <= connectAttempts {
			p.Exit(errors.New("workspace is stopped"))
		}
	}

	host, err := h.svc.Activate(context.Background(), &sess, false)
	require.NoError(t, err)
	assert.Equal(t, "ws", host)
	assert.Contains(t, h.runner.Runs(), "coder start ws -y")
}

func TestService_ActivateWithoutRemoteFails(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, nil)
	h := newHarness(t, sess)
	_, err := h.svc.Activate(context.Background(), &sess, false)
	assert.ErrorIs(t, err, ErrNoRemoteSession)
}

func TestService_ArchiveArchivesFollowerAndStops(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusArchived, remoteID(42))
	h := newHarness(t, sess)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	proc := h.runner.Proc(0)

	require.NoError(t, h.svc.Archive(context.Background(), &sess))
	assert.Len(t, h.follower.CallsTo("PUT", "/sessions/42/archive"), 1)
	assert.True(t, proc.killed())
	assert.Equal(t, []string{"tuios hosts add ws ws.coder", "tuios hosts remove ws", "coder stop ws -y"}, h.runner.Runs())
	_, connected := h.conns.Connected("ws")
	assert.False(t, connected)
}

func TestService_ArchiveUnreachableFollowerStillStops(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusArchived, remoteID(42))
	h := newHarness(t, sess)
	h.runner.startErr = errors.New("no ssh")
	require.NoError(t, h.svc.Archive(context.Background(), &sess))
	assert.Contains(t, h.runner.Runs(), "coder stop ws -y")
}

func TestService_DeleteTearsDownEverything(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusDeleted, remoteID(42))
	h := newHarness(t, sess)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)

	require.NoError(t, h.svc.Delete(context.Background(), &sess))
	assert.Len(t, h.follower.CallsTo("DELETE", "/sessions/42?force=true"), 1)
	assert.Equal(t, []string{
		"tuios hosts add ws ws.coder",
		"tuios hosts remove ws",
		"coder tokens remove utena-ws",
		"coder delete ws -y",
	}, h.runner.Runs())
}

func TestService_DeleteSkipsFollowerWhenDisconnected(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusDeleted, remoteID(42))
	h := newHarness(t, sess)
	h.runner.handle = coderHandler(func(c Command) ([]byte, error, bool) {
		if c.Name == "coder" && c.Args[0] == "tokens" {
			return nil, errors.New("token not found"), true
		}
		return nil, nil, false
	})
	require.NoError(t, h.svc.Delete(context.Background(), &sess))
	assert.Empty(t, h.follower.Calls())
	assert.Equal(t, 0, h.runner.StartCount())
	assert.Contains(t, h.runner.Runs(), "coder delete ws -y")
}

func TestService_DeleteSurfacesCoderDeleteError(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusDeleted, nil)
	h := newHarness(t, sess)
	h.runner.handle = coderHandler(func(c Command) ([]byte, error, bool) {
		if c.Name == "coder" && c.Args[0] == "delete" {
			return nil, errors.New("permission denied"), true
		}
		return nil, nil, false
	})
	err := h.svc.Delete(context.Background(), &sess)
	require.ErrorContains(t, err, "permission denied")
}

func TestService_DeleteTreatsMissingWorkspaceAsDeleted(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusDeleted, nil)
	h := newHarness(t, sess)
	h.runner.handle = func(c Command) ([]byte, error) {
		if c.Name == "coder" && c.Args[0] == "delete" {
			return nil, errors.New("not found")
		}
		if c.Name == "coder" && c.Args[0] == "list" {
			return []byte(`[]`), nil
		}
		return nil, nil
	}
	require.NoError(t, h.svc.Delete(context.Background(), &sess))
}

func TestService_PollMirrorsStateWhileConnected(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, remoteID(42))
	h := newHarness(t, sess)
	h.follower.set(func(f *fakeFollower) {
		f.attention = claude.StatusReadyForReview
		f.statusErr = "warning"
		f.branches = []string{"eqt/widgets", "eqt/widgets-v2"}
	})

	h.svc.poll(context.Background())
	_, ok := h.sessions.lastState(9)
	assert.False(t, ok, "nothing is polled while disconnected")

	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	h.svc.poll(context.Background())
	st, ok := h.sessions.lastState(9)
	require.True(t, ok)
	assert.Equal(t, session.StatusActive, st.Status)
	assert.Equal(t, "warning", st.StatusError)
	assert.Equal(t, claude.StatusReadyForReview, st.Attention)
	assert.Equal(t, []string{"eqt/widgets", "eqt/widgets-v2"}, st.Branches)
	assert.Equal(t, h.clock.Now(), st.SyncedAt)
}

func TestService_PollSkipsArchivedAndCreating(t *testing.T) {
	h := newHarness(t,
		coderSession(1, "ws", session.StatusArchived, remoteID(42)),
		coderSession(2, "ws", session.StatusCreating, remoteID(42)),
	)
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)
	h.svc.poll(context.Background())
	assert.Empty(t, h.follower.CallsTo("GET", "/sessions/"))
}

func TestService_HoldsConnectionWhileBusyOrAttached(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, remoteID(42))
	h := newHarness(t, sess)
	h.follower.set(func(f *fakeFollower) { f.attention = claude.StatusWorking })
	_, err := h.conns.Ensure(context.Background(), "ws")
	require.NoError(t, err)

	h.clock.Advance(11 * time.Minute)
	h.svc.poll(context.Background())
	_, connected := h.conns.Connected("ws")
	assert.True(t, connected, "a working agent holds the connection")

	h.follower.set(func(f *fakeFollower) { f.attention = claude.StatusNeedsAttention })
	h.clock.Advance(11 * time.Minute)
	h.svc.poll(context.Background())
	_, connected = h.conns.Connected("ws")
	assert.True(t, connected, "an agent waiting for input holds the connection")

	h.follower.set(func(f *fakeFollower) { f.attention = claude.StatusIdle })
	h.sessions.setAttached(9, true)
	h.clock.Advance(11 * time.Minute)
	h.svc.poll(context.Background())
	_, connected = h.conns.Connected("ws")
	assert.True(t, connected, "an attached session holds the connection")

	h.sessions.setAttached(9, false)
	h.clock.Advance(9 * time.Minute)
	h.svc.poll(context.Background())
	_, connected = h.conns.Connected("ws")
	assert.True(t, connected, "idle for less than idle_disconnect")

	h.clock.Advance(2 * time.Minute)
	h.svc.poll(context.Background())
	_, connected = h.conns.Connected("ws")
	assert.False(t, connected, "idle past idle_disconnect drops the connection")
	assert.True(t, h.runner.Proc(0).killed())
	assert.Equal(t, "tuios hosts remove ws", h.runner.Runs()[len(h.runner.Runs())-1])

	st, _ := h.sessions.lastState(9)
	assert.Equal(t, claude.StatusIdle, st.Attention, "last-known state is kept while disconnected")
}

func TestService_RefreshReconnects(t *testing.T) {
	sess := coderSession(9, "ws", session.StatusActive, remoteID(42))
	h := newHarness(t, sess)
	require.NoError(t, h.svc.Refresh(context.Background(), 9))
	_, connected := h.conns.Connected("ws")
	assert.True(t, connected)
	_, ok := h.sessions.lastState(9)
	assert.True(t, ok)
	assert.Error(t, h.svc.Refresh(context.Background(), 404))
}

func TestFollowerSession_DecodesFollowerResponse(t *testing.T) {
	resp := session.NewSessionResponse(&session.Session{Model: gorm.Model{ID: 5}, Status: session.StatusActive, ClaudeSessions: []claude.ClaudeSession{{Status: claude.StatusWorking}}})
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(data), `"ID":5`))
	var fs FollowerSession
	require.NoError(t, json.Unmarshal(data, &fs))
	assert.Equal(t, uint(5), fs.ID)
	assert.Equal(t, claude.StatusWorking, fs.AttentionStatus)
}
