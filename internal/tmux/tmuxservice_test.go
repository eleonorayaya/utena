package tmux

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTmuxService_LockNameSerializesSameName(t *testing.T) {
	svc := &TmuxService{}

	unlock1 := svc.lockName("foo")

	acquired := make(chan struct{})
	go func() {
		unlock2 := svc.lockName("foo")
		close(acquired)
		unlock2()
	}()

	select {
	case <-acquired:
		t.Fatal("second lockName(\"foo\") acquired while first still held")
	case <-time.After(50 * time.Millisecond):
	}

	unlock1()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("second lockName(\"foo\") did not unblock after first released")
	}
}

func TestTmuxService_LockNameDoesNotBlockDifferentNames(t *testing.T) {
	svc := &TmuxService{}

	unlockFoo := svc.lockName("foo")
	defer unlockFoo()

	acquired := make(chan struct{})
	go func() {
		unlockBar := svc.lockName("bar")
		close(acquired)
		unlockBar()
	}()

	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("lockName(\"bar\") blocked behind lockName(\"foo\")")
	}
}

func TestTmuxService_ConcurrentKillCreateNoOverlap(t *testing.T) {
	database := testdb.New(t, &TmuxSession{})

	bus := eventbus.NewEventBus()
	mock := NewMockRunner()
	store := NewTmuxStore(database)
	svc := NewTmuxService(mock, store, bus)

	var inFlight int32
	var maxInFlight int32
	mock.OnNewSession = func() {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			cur := atomic.LoadInt32(&maxInFlight)
			if n <= cur || atomic.CompareAndSwapInt32(&maxInFlight, cur, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
	}
	mock.OnKillSession = func() {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			cur := atomic.LoadInt32(&maxInFlight)
			if n <= cur || atomic.CompareAndSwapInt32(&maxInFlight, cur, n) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		atomic.AddInt32(&inFlight, -1)
	}

	const goroutines = 20
	done := make(chan struct{}, goroutines)
	for range goroutines {
		go func() {
			defer func() { done <- struct{}{} }()
			ts, err := svc.CreateSession("shared", "/tmp", nil)
			if err == nil {
				_ = svc.KillSession(ts.ID)
			}
		}()
	}
	for range goroutines {
		<-done
	}

	require.Equal(t, int32(1), atomic.LoadInt32(&maxInFlight),
		"per-name lock should keep at most one tmux op in flight for the same name")
}

func newTestService(t *testing.T) *TmuxService {
	t.Helper()
	database := testdb.New(t, &TmuxSession{})
	store := NewTmuxStore(database)
	return NewTmuxService(NewMockRunner(), store, eventbus.NewEventBus())
}

func TestTmuxService_RegisterPending_CreatesPendingRecord(t *testing.T) {
	svc := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", map[string]string{"FOO": "bar"})
	require.NoError(t, err)
	assert.Equal(t, "name", ts.Name)
	assert.Equal(t, "/start", ts.StartDir)
	assert.Equal(t, TmuxStatusPending, ts.Status)
	assert.Equal(t, "bar", ts.Env["FOO"])
}

func TestTmuxService_RegisterPending_IdempotentOnPending(t *testing.T) {
	svc := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	second, err := svc.RegisterPending("name", "/different", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "/start", second.StartDir, "RegisterPending must not mutate StartDir on retry")
}

func TestTmuxService_RegisterPending_IdempotentOnInactive(t *testing.T) {
	svc := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	first.Status = TmuxStatusInactive
	require.NoError(t, svc.store.Update(first))

	second, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, TmuxStatusInactive, second.Status, "RegisterPending must not flip an inactive record back to pending")
}

func TestTmuxService_RegisterPending_AdoptsActiveRecord(t *testing.T) {
	// When a tmux record exists in Active status (e.g. previous owner deleted
	// itself without killing the tmux process, or the daemon was off and missed
	// the close hook), RegisterPending returns the existing record so the new
	// owner can adopt it. Real "another session is already linked" conflicts
	// are caught at the caller's Session.TmuxSessionID unique-constraint write.
	svc := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	first.Status = TmuxStatusActive
	require.NoError(t, svc.store.Update(first))

	second, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "adoption returns the same record")
	assert.Equal(t, TmuxStatusActive, second.Status, "status preserved on adoption")
}

func TestTmuxService_SpawnForRecord_TransitionsPendingToActive(t *testing.T) {
	svc := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	spawned, err := svc.SpawnForRecord(ts.ID)
	require.NoError(t, err)
	assert.Equal(t, TmuxStatusActive, spawned.Status)

	mock := svc.runner.(*MockRunner)
	assert.True(t, mock.HasSessionByName("name"))
}

func TestTmuxService_SpawnForRecord_DoubleSpawnIsSafe(t *testing.T) {
	svc := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	_, err = svc.SpawnForRecord(ts.ID)
	require.NoError(t, err)

	_, err = svc.SpawnForRecord(ts.ID)
	require.NoError(t, err, "spawn should be idempotent if tmux process already exists by name")
}

func TestTmuxService_SessionClosedAfterUtenaKillIsNotPublished(t *testing.T) {
	database := testdb.New(t, &TmuxSession{})
	bus := eventbus.NewEventBus()
	svc := NewTmuxService(NewMockRunner(), NewTmuxStore(database), bus)

	var published int32
	bus.Subscribe(eventbus.TmuxSessionClosed, func(ctx context.Context, e eventbus.Event) error {
		atomic.AddInt32(&published, 1)
		return nil
	})

	ts, err := svc.CreateSession("work", "/tmp", nil)
	require.NoError(t, err)
	require.NoError(t, svc.KillSession(ts.ID))
	require.NoError(t, svc.HandleSessionClosed(context.Background(), "work"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&published), "utena-initiated kill must not surface as an external close")

	_, err = svc.CreateSession("other", "/tmp", nil)
	require.NoError(t, err)
	require.NoError(t, svc.HandleSessionClosed(context.Background(), "other"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&published), "external close of an active session is published")
}

func TestTmuxService_WaitForActivationReceivesActivatedSession(t *testing.T) {
	svc := newTestService(t)
	bus := svc.eventBus

	got := make(chan string, 1)
	go func() {
		name, _ := svc.WaitForActivation(context.Background())
		got <- name
	}()
	require.Eventually(t, func() bool {
		svc.activationsMu.Lock()
		defer svc.activationsMu.Unlock()
		return len(svc.activationWaiters) == 1
	}, time.Second, time.Millisecond)

	require.NoError(t, bus.Publish(context.Background(), eventbus.Event{
		Type: eventbus.SessionActivated,
		Data: eventbus.SessionActivatedEvent{SessionName: "work"},
	}))
	assert.Equal(t, "work", <-got)
}

func TestTmuxService_WaitForActivationTimesOut(t *testing.T) {
	svc := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := svc.WaitForActivation(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, svc.activationWaiters)
}
