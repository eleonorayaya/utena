package mux

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

func TestService_LockNameSerializesSameName(t *testing.T) {
	svc := &Service{}

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

func TestService_LockNameDoesNotBlockDifferentNames(t *testing.T) {
	svc := &Service{}

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

func TestService_ConcurrentKillCreateNoOverlap(t *testing.T) {
	database := testdb.New(t, &Session{})

	bus := eventbus.NewEventBus()
	mock := NewFakeDriver()
	store := NewStore(database)
	svc := NewService(mock, store, bus)

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
		"per-name lock should keep at most one mux op in flight for the same name")
}

func newTestService(t *testing.T) (*Service, *FakeDriver) {
	t.Helper()
	database := testdb.New(t, &Session{})
	driver := NewFakeDriver()
	return NewService(driver, NewStore(database), eventbus.NewEventBus()), driver
}

func TestService_RegisterPending_CreatesPendingRecord(t *testing.T) {
	svc, _ := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", map[string]string{"FOO": "bar"})
	require.NoError(t, err)
	assert.Equal(t, "name", ts.Name)
	assert.Equal(t, "/start", ts.StartDir)
	assert.Equal(t, StatusPending, ts.Status)
	assert.Equal(t, "bar", ts.Env["FOO"])
}

func TestService_RegisterPending_IdempotentOnPending(t *testing.T) {
	svc, _ := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	second, err := svc.RegisterPending("name", "/different", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "/start", second.StartDir, "RegisterPending must not mutate StartDir on retry")
}

func TestService_RegisterPending_IdempotentOnInactive(t *testing.T) {
	svc, _ := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	first.Status = StatusInactive
	require.NoError(t, svc.store.Update(first))

	second, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, StatusInactive, second.Status, "RegisterPending must not flip an inactive record back to pending")
}

func TestService_RegisterPending_AdoptsActiveRecord(t *testing.T) {
	svc, _ := newTestService(t)

	first, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	first.Status = StatusActive
	require.NoError(t, svc.store.Update(first))

	second, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)
	assert.Equal(t, first.ID, second.ID, "adoption returns the same record")
	assert.Equal(t, StatusActive, second.Status, "status preserved on adoption")
}

func TestService_SpawnForRecord_TransitionsPendingToActive(t *testing.T) {
	svc, driver := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	spawned, err := svc.SpawnForRecord(ts.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusActive, spawned.Status)

	assert.True(t, driver.HasSession("name"))
}

func TestService_SpawnForRecord_DoubleSpawnIsSafe(t *testing.T) {
	svc, _ := newTestService(t)

	ts, err := svc.RegisterPending("name", "/start", nil)
	require.NoError(t, err)

	_, err = svc.SpawnForRecord(ts.ID)
	require.NoError(t, err)

	_, err = svc.SpawnForRecord(ts.ID)
	require.NoError(t, err, "spawn should be idempotent if mux session already exists by name")
}

func TestService_SessionClosedAfterUtenaKillIsNotPublished(t *testing.T) {
	database := testdb.New(t, &Session{})
	bus := eventbus.NewEventBus()
	svc := NewService(NewFakeDriver(), NewStore(database), bus)

	var published int32
	bus.Subscribe(eventbus.MuxSessionClosed, func(ctx context.Context, e eventbus.Event) error {
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

func TestService_WaitForActivationReceivesActivatedSession(t *testing.T) {
	svc, _ := newTestService(t)
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

func TestService_WaitForActivationTimesOut(t *testing.T) {
	svc, _ := newTestService(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := svc.WaitForActivation(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Empty(t, svc.activationWaiters)
}

func startFollowing(t *testing.T) (*Service, *FakeDriver, eventbus.EventBus) {
	t.Helper()
	svc, driver := newTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go svc.followEvents(ctx)
	return svc, driver, svc.eventBus
}

func emit(t *testing.T, driver *FakeDriver, ev Event) {
	t.Helper()
	require.Eventually(t, func() bool { return driver.Emit(ev) }, time.Second, time.Millisecond)
}

func TestService_ResyncReconcilesStatusAndRefreshesWindows(t *testing.T) {
	svc, driver, _ := startFollowing(t)

	gone := &Session{Name: "gone", Status: StatusActive}
	back := &Session{Name: "back", Status: StatusInactive}
	require.NoError(t, svc.store.Add(gone))
	require.NoError(t, svc.store.Add(back))
	require.NoError(t, driver.NewSession("back", "/tmp", nil))
	driver.SetWindows("back", []Window{{Index: 0, Name: "zsh", Active: true}})

	emit(t, driver, Event{Type: EventResync})

	got, err := svc.GetSessionByName("gone")
	require.NoError(t, err)
	assert.Equal(t, StatusInactive, got.Status)
	got, err = svc.GetSessionByName("back")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)
	assert.Equal(t, []Window{{Index: 0, Name: "zsh", Active: true}}, got.Windows)
}

func TestService_SessionCreatedActivatesPublishesAndRefreshesWindows(t *testing.T) {
	svc, driver, bus := startFollowing(t)

	var published []string
	bus.Subscribe(eventbus.MuxSessionCreated, func(ctx context.Context, e eventbus.Event) error {
		published = append(published, e.Data.(eventbus.MuxHookEvent).SessionName)
		return nil
	})
	require.NoError(t, svc.store.Add(&Session{Name: "work", Status: StatusPending}))
	driver.SetWindows("work", []Window{{Index: 0, Name: "claude"}})

	emit(t, driver, Event{Type: EventSessionCreated, Session: "work"})

	got, err := svc.GetSessionByName("work")
	require.NoError(t, err)
	assert.Equal(t, StatusActive, got.Status)
	assert.Equal(t, []string{"work"}, published)
	assert.Equal(t, []Window{{Index: 0, Name: "claude"}}, svc.GetWindows(context.Background(), "work"))
}

func TestService_WindowsChangedRefreshesWindows(t *testing.T) {
	svc, driver, _ := startFollowing(t)

	driver.SetWindows("work", []Window{{Index: 0, Name: "zsh"}})
	emit(t, driver, Event{Type: EventWindowsChanged, Session: "work"})
	assert.Equal(t, []Window{{Index: 0, Name: "zsh"}}, svc.GetWindows(context.Background(), "work"))

	driver.SetWindows("work", []Window{{Index: 0, Name: "zsh"}, {Index: 1, Name: "claude", Active: true}})
	emit(t, driver, Event{Type: EventWindowsChanged, Session: "work"})
	assert.Equal(t, []Window{{Index: 0, Name: "zsh"}, {Index: 1, Name: "claude", Active: true}}, svc.GetWindows(context.Background(), "work"))
}
