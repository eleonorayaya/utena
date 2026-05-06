package tmux

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
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
			} else {
				_ = svc.KillSessionByName("shared")
			}
		}()
	}
	for range goroutines {
		<-done
	}

	require.Equal(t, int32(1), atomic.LoadInt32(&maxInFlight),
		"per-name lock should keep at most one tmux op in flight for the same name")
}
