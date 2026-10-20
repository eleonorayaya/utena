package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/claude"
	"github.com/eleonorayaya/utena/internal/common"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/git"
	"github.com/eleonorayaya/utena/internal/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBackend struct {
	mu          sync.Mutex
	enabled     bool
	creates     []RemoteCreate
	activations []bool
	archived    []uint
	deleted     []uint
	createErr   error
	activateErr error
	deleteErr   error
	release     chan struct{}
}

func (b *fakeBackend) Enabled() bool { return b.enabled }

func (b *fakeBackend) WorkspaceName(name string, taken []string) (string, error) {
	candidate := SanitizeSessionName(name)
	for _, t := range taken {
		if t == candidate {
			return candidate + "-2", nil
		}
	}
	return candidate, nil
}

func (b *fakeBackend) wait() {
	if b.release != nil {
		<-b.release
	}
}

func (b *fakeBackend) Create(_ context.Context, in RemoteCreate) error {
	b.wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.creates = append(b.creates, in)
	return b.createErr
}

func (b *fakeBackend) Activate(_ context.Context, sess *Session, resume bool) (string, error) {
	b.wait()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.activations = append(b.activations, resume)
	return sess.CoderWorkspace, b.activateErr
}

func (b *fakeBackend) Archive(_ context.Context, sess *Session) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.archived = append(b.archived, sess.ID)
	return nil
}

func (b *fakeBackend) Delete(_ context.Context, sess *Session) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.deleted = append(b.deleted, sess.ID)
	return b.deleteErr
}

func (b *fakeBackend) snapshot() fakeBackend {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fakeBackend{creates: append([]RemoteCreate(nil), b.creates...), activations: append([]bool(nil), b.activations...), archived: append([]uint(nil), b.archived...), deleted: append([]uint(nil), b.deleted...)}
}

type coderFixture struct {
	svc     *SessionService
	store   *SessionStore
	backend *fakeBackend
	wsID    uint
	ws2ID   uint
}

func setupCoderService(t *testing.T) coderFixture {
	t.Helper()
	service, store, wsStore, _, _, _ := setupSessionService(t)
	repo := &git.Repo{Path: "/tmp/widgets", FullName: "acme/widgets"}
	require.NoError(t, store.db.Create(repo).Error)
	ws := &workspace.Workspace{Name: "widgets", Path: "/tmp/widgets", IsGitRepo: true, RepoID: &repo.ID}
	require.NoError(t, wsStore.Add(ws))
	repo2 := &git.Repo{Path: "/tmp/gadgets", FullName: "acme/gadgets"}
	require.NoError(t, store.db.Create(repo2).Error)
	ws2 := &workspace.Workspace{Name: "gadgets", Path: "/tmp/gadgets", IsGitRepo: true, RepoID: &repo2.ID}
	require.NoError(t, wsStore.Add(ws2))
	backend := &fakeBackend{enabled: true}
	service.SetRemoteBackend(backend)
	return coderFixture{svc: service, store: store, backend: backend, wsID: ws.ID, ws2ID: ws2.ID}
}

func (f coderFixture) create(t *testing.T, name string) *Session {
	t.Helper()
	sess, err := f.svc.CreateSession(context.Background(), CreateSessionInput{
		Name:       name,
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: f.wsID, BaseBranch: "main"}},
		Backend:    BackendCoder,
	})
	require.NoError(t, err)
	return sess
}

func addCoderSession(t *testing.T, store *SessionStore, name string, status SessionStatus) *Session {
	t.Helper()
	remote := uint(42)
	sess := &Session{Name: name, Status: status, LastUsedAt: time.Now(), Backend: BackendCoder, CoderWorkspace: name}
	require.NoError(t, store.Add(sess))
	require.NoError(t, store.SetRemoteSessionID(sess.ID, remote))
	got, err := store.GetByID(sess.ID)
	require.NoError(t, err)
	return got
}

func requireInvalid(t *testing.T, err error) {
	t.Helper()
	var appErr *common.AppError
	require.True(t, errors.As(err, &appErr), "expected an AppError, got %v", err)
	assert.Equal(t, common.CategoryInvalidRequest, appErr.Category)
}

func TestCoderCreate_RejectsWhenNotConfigured(t *testing.T) {
	f := setupCoderService(t)
	f.backend.enabled = false
	_, err := f.svc.CreateSession(context.Background(), CreateSessionInput{Name: "x", Workspaces: []WorkspaceBranchSpec{{WorkspaceID: f.wsID, BaseBranch: "main"}}, Backend: BackendCoder})
	requireInvalid(t, err)
	assert.ErrorIs(t, err, ErrCoderNotConfigured)
}

func TestCoderCreate_RejectsMultipleWorkspaces(t *testing.T) {
	f := setupCoderService(t)
	_, err := f.svc.CreateSession(context.Background(), CreateSessionInput{
		Name:       "x",
		Workspaces: []WorkspaceBranchSpec{{WorkspaceID: f.wsID, BaseBranch: "main"}, {WorkspaceID: f.ws2ID, BaseBranch: "main"}},
		Backend:    BackendCoder,
	})
	requireInvalid(t, err)
}

func TestCoderCreate_RejectsUnknownBackend(t *testing.T) {
	f := setupCoderService(t)
	_, err := f.svc.CreateSession(context.Background(), CreateSessionInput{Name: "x", Workspaces: []WorkspaceBranchSpec{{WorkspaceID: f.wsID, BaseBranch: "main"}}, Backend: "cloud"})
	requireInvalid(t, err)
}

func TestCoderCreate_CreatesNoLocalResources(t *testing.T) {
	f := setupCoderService(t)
	sess := f.create(t, "Fix_Login")

	assert.Equal(t, BackendCoder, sess.Backend)
	assert.Equal(t, "fix_login", sess.CoderWorkspace)
	assert.Empty(t, sess.SessionRoot)
	assert.Nil(t, sess.MuxSessionID)
	waitForStatus(t, f.store, sess.ID, StatusActive, 5*time.Second)

	got, err := f.store.GetByID(sess.ID)
	require.NoError(t, err)
	assert.Empty(t, got.Worktrees)
	assert.Empty(t, got.SetupSteps)
	assert.Nil(t, got.MuxSessionID)

	creates := f.backend.snapshot().creates
	require.Len(t, creates, 1)
	assert.Equal(t, RemoteCreate{
		SessionID:         sess.ID,
		Workspace:         "fix_login",
		Repo:              "acme/widgets",
		LeaderWorkspaceID: f.wsID,
		Spec:              WorkspaceBranchSpec{WorkspaceID: f.wsID, BaseBranch: "main"},
	}, creates[0])
}

func TestCoderCreate_SlugAvoidsExistingWorkspaces(t *testing.T) {
	f := setupCoderService(t)
	first := f.create(t, "fix")
	waitForStatus(t, f.store, first.ID, StatusActive, 5*time.Second)
	second := f.create(t, "fix")
	assert.Equal(t, "fix", first.CoderWorkspace)
	assert.Equal(t, "fix-2", second.CoderWorkspace)
}

func TestCoderCreate_FailureMarksBroken(t *testing.T) {
	f := setupCoderService(t)
	f.backend.createErr = errors.New("coder create: quota exceeded")
	sess := f.create(t, "fix")
	waitForStatus(t, f.store, sess.ID, StatusBroken, 5*time.Second)
	got, err := f.store.GetByID(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, "coder create: quota exceeded", got.StatusError)
}

func TestCoderSession_SkipsLocalStartupChecks(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	require.NoError(t, f.svc.OnAppStart(context.Background()))
	got, err := f.store.GetByID(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status, "zero worktrees and no mux record are normal for coder sessions")

	reconciled, err := f.svc.ReconcileSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, reconciled.Status)
}

func TestCoderActivate_PublishesHostAfterBackend(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	f.backend.release = make(chan struct{})
	var activations []eventbus.SessionActivatedEvent
	var mu sync.Mutex
	f.svc.eventBus.Subscribe(eventbus.SessionActivated, func(_ context.Context, e eventbus.Event) error {
		mu.Lock()
		defer mu.Unlock()
		activations = append(activations, e.Data.(eventbus.SessionActivatedEvent))
		return nil
	})

	got, err := f.svc.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.False(t, got.IsAttached)
	mu.Lock()
	assert.Empty(t, activations, "nothing is published before the backend is ready")
	mu.Unlock()

	close(f.backend.release)
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(activations) == 1
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, eventbus.SessionActivatedEvent{SessionName: "ws", Host: "ws"}, activations[0])
	attached, err := f.store.IsAttached(sess.ID)
	require.NoError(t, err)
	assert.True(t, attached)
	assert.Equal(t, []bool{false}, f.backend.snapshot().activations)
}

func TestCoderActivate_ResumesArchived(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusArchived)
	got, err := f.svc.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusCreating, got.Status)
	waitForStatus(t, f.store, sess.ID, StatusActive, 5*time.Second)
	assert.Equal(t, []bool{true}, f.backend.snapshot().activations)
}

func TestCoderActivate_ResumeFailureReturnsToArchived(t *testing.T) {
	f := setupCoderService(t)
	f.backend.activateErr = errors.New("coder start: boom")
	sess := addCoderSession(t, f.store, "ws", StatusArchived)
	_, err := f.svc.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		got, err := f.store.GetByID(sess.ID)
		return err == nil && got.Status == StatusArchived && got.StatusError != ""
	}, 5*time.Second, 5*time.Millisecond)
}

func TestCoderActivate_LocalActivationUnchanged(t *testing.T) {
	f := setupCoderService(t)
	var activations []eventbus.SessionActivatedEvent
	f.svc.eventBus.Subscribe(eventbus.SessionActivated, func(_ context.Context, e eventbus.Event) error {
		activations = append(activations, e.Data.(eventbus.SessionActivatedEvent))
		return nil
	})
	sess := addAttachableSession(t, f.svc, f.store, f.wsID, "local-one")
	_, err := f.svc.ActivateSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.Len(t, activations, 1)
	assert.Equal(t, eventbus.SessionActivatedEvent{SessionName: "local-one"}, activations[0])
	assert.Empty(t, f.backend.snapshot().activations)
}

func TestCoderArchive_ArchivesAndCallsBackend(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	got, err := f.svc.ArchiveSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusArchived, got.Status)
	require.Eventually(t, func() bool { return len(f.backend.snapshot().archived) == 1 }, 5*time.Second, 5*time.Millisecond)
}

func TestCoderDelete_RemovesRowAfterBackend(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusArchived)
	require.NoError(t, f.svc.DeleteSession(context.Background(), sess.ID, true, false))
	require.Eventually(t, func() bool {
		_, err := f.store.GetByID(sess.ID)
		return errors.Is(err, ErrSessionNotFound)
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, []uint{sess.ID}, f.backend.snapshot().deleted)
}

func TestCoderDelete_FailureSurfacesOnArchivedSession(t *testing.T) {
	f := setupCoderService(t)
	f.backend.deleteErr = errors.New("coder delete: permission denied")
	sess := addCoderSession(t, f.store, "ws", StatusArchived)
	require.NoError(t, f.svc.DeleteSession(context.Background(), sess.ID, true, false))
	require.Eventually(t, func() bool {
		got, err := f.store.GetByID(sess.ID)
		return err == nil && got.Status == StatusArchived && got.StatusError == "delete failed: coder delete: permission denied"
	}, 5*time.Second, 5*time.Millisecond)
}

func TestCoderDelete_RequiresArchived(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	assert.ErrorIs(t, f.svc.DeleteSession(context.Background(), sess.ID, true, false), ErrSessionNotArchived)
}

func TestCoderSession_RejectsLocalOnlyOperations(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	_, err := f.svc.SetBundles(context.Background(), sess.ID, []string{"go"})
	assert.ErrorIs(t, err, ErrCoderUnsupported)
	_, err = f.svc.AddWorkspace(context.Background(), sess.ID, WorkspaceBranchSpec{WorkspaceID: f.ws2ID, BaseBranch: "main"})
	assert.ErrorIs(t, err, ErrCoderUnsupported)
	require.NoError(t, f.store.SetStatusError(sess.ID, "warn", StatusActive))
	_, err = f.svc.RepairSession(context.Background(), sess.ID)
	assert.ErrorIs(t, err, ErrCoderUnsupported)
}

func TestCoderHook_RoutesByHost(t *testing.T) {
	f := setupCoderService(t)
	local := addAttachableSession(t, f.svc, f.store, f.wsID, "ws")
	remote := addCoderSession(t, f.store, "ws", StatusActive)
	got := recordSessionAttached(f.svc)

	require.NoError(t, f.svc.handleMuxClientSessionChanged(context.Background(), eventbus.Event{
		Type: eventbus.MuxClientSessionChanged,
		Data: eventbus.MuxHookEvent{SessionName: "ws", Host: "ws"},
	}))
	assert.Equal(t, []eventbus.SessionIDEvent{{SessionID: remote.ID}}, got())

	require.NoError(t, f.svc.handleMuxClientDetached(context.Background(), eventbus.Event{
		Type: eventbus.MuxClientDetached,
		Data: eventbus.MuxHookEvent{SessionName: "ws", Host: "ws"},
	}))
	attached, err := f.store.IsAttached(remote.ID)
	require.NoError(t, err)
	assert.False(t, attached)

	require.NoError(t, f.svc.handleMuxClientSessionChanged(context.Background(), eventbus.Event{
		Type: eventbus.MuxClientSessionChanged,
		Data: eventbus.MuxHookEvent{SessionName: "ws"},
	}))
	assert.Equal(t, []eventbus.SessionIDEvent{{SessionID: remote.ID}, {SessionID: local.ID}}, got())
}

func TestCoderRemoteState_MirroredIntoResponses(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, f.svc.ApplyRemoteState(sess.ID, RemoteState{
		Status:       StatusActive,
		StatusError:  "warning",
		Attention:    claude.StatusNeedsAttention,
		PullRequests: []git.PullRequest{{Number: 17, Title: "Add widgets", HTMLURL: "https://example.com/pr/17"}},
		SyncedAt:     now,
	}))

	got, err := f.svc.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	assert.Equal(t, claude.StatusNeedsAttention, got.AttentionStatus())
	assert.Equal(t, "warning", got.StatusError)
	require.Len(t, got.PullRequests, 1)
	assert.Equal(t, 17, got.PullRequests[0].Number)
	require.NotNil(t, got.RemoteSyncedAt)
	assert.True(t, now.Equal(*got.RemoteSyncedAt))

	list, err := f.svc.ListSessions(context.Background())
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 17, list[0].PullRequests[0].Number)
	assert.Equal(t, "coder · ws", list[0].WorkspaceDisplay())
}

func TestCoderRemoteState_NeverOverridesLeaderLifecycle(t *testing.T) {
	f := setupCoderService(t)
	for _, status := range []SessionStatus{StatusArchived, StatusCreating, StatusDeleted} {
		sess := addCoderSession(t, f.store, "ws-"+string(status), status)
		require.NoError(t, f.svc.ApplyRemoteState(sess.ID, RemoteState{Status: StatusActive, Attention: claude.StatusIdle, SyncedAt: time.Now()}))
		got, err := f.store.GetByID(sess.ID)
		require.NoError(t, err)
		assert.Equal(t, status, got.Status)
		assert.Equal(t, claude.StatusIdle, got.RemoteAttention)
	}
	sess := addCoderSession(t, f.store, "ws-active", StatusActive)
	require.NoError(t, f.svc.ApplyRemoteState(sess.ID, RemoteState{Status: StatusPending, SyncedAt: time.Now()}))
	got, err := f.store.GetByID(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status, "statuses the leader doesn't mirror are ignored")
}

func TestCoderRemoteState_SurvivesFullSave(t *testing.T) {
	f := setupCoderService(t)
	sess := addCoderSession(t, f.store, "ws", StatusActive)
	require.NoError(t, f.svc.ApplyRemoteState(sess.ID, RemoteState{Status: StatusActive, Attention: claude.StatusWorking, SyncedAt: time.Now()}))
	sess.IsAttached = true
	require.NoError(t, f.store.Update(sess))
	got, err := f.store.GetByID(sess.ID)
	require.NoError(t, err)
	assert.Equal(t, claude.StatusWorking, got.RemoteAttention)
	require.NotNil(t, got.RemoteSessionID)
	assert.Equal(t, uint(42), *got.RemoteSessionID)
}
