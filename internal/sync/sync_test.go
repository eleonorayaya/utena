package sync

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type mockTask struct {
	name     string
	interval time.Duration
	runCount atomic.Int32
	err      error
}

func (m *mockTask) Name() string            { return m.name }
func (m *mockTask) Interval() time.Duration { return m.interval }
func (m *mockTask) Run(_ context.Context) error {
	m.runCount.Add(1)
	return m.err
}

func TestTaskRunsAtInterval(t *testing.T) {
	mgr := NewSyncManager()
	task := &mockTask{name: "ticker", interval: 50 * time.Millisecond}
	mgr.Register(task)
	mgr.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	mgr.Stop()

	count := task.runCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 runs, got %d", count)
	}
}

func TestStopCancelsRunningTasks(t *testing.T) {
	mgr := NewSyncManager()
	task := &mockTask{name: "stopper", interval: 50 * time.Millisecond}
	mgr.Register(task)
	mgr.Start(context.Background())

	time.Sleep(100 * time.Millisecond)
	mgr.Stop()

	countAfterStop := task.runCount.Load()
	time.Sleep(150 * time.Millisecond)
	countLater := task.runCount.Load()

	if countLater != countAfterStop {
		t.Errorf("task continued running after stop: %d -> %d", countAfterStop, countLater)
	}
}

func TestTriggerSyncRunsTaskImmediately(t *testing.T) {
	mgr := NewSyncManager()
	task := &mockTask{name: "manual", interval: 10 * time.Second}
	mgr.Register(task)
	mgr.Start(context.Background())
	defer mgr.Stop()

	time.Sleep(20 * time.Millisecond)
	initialCount := task.runCount.Load()

	if err := mgr.TriggerSync("manual"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if task.runCount.Load() <= initialCount {
		t.Error("expected task to run after trigger")
	}
}

func TestTaskErrorsDoNotCrashManager(t *testing.T) {
	mgr := NewSyncManager()
	failing := &mockTask{name: "failing", interval: 50 * time.Millisecond, err: errors.New("boom")}
	healthy := &mockTask{name: "healthy", interval: 50 * time.Millisecond}
	mgr.Register(failing)
	mgr.Register(healthy)
	mgr.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	mgr.Stop()

	if failing.runCount.Load() < 2 {
		t.Error("failing task should have continued running despite errors")
	}
	if healthy.runCount.Load() < 2 {
		t.Error("healthy task should have kept running")
	}
}

func TestMultipleTasksRunIndependently(t *testing.T) {
	mgr := NewSyncManager()
	task1 := &mockTask{name: "task1", interval: 50 * time.Millisecond}
	task2 := &mockTask{name: "task2", interval: 50 * time.Millisecond}
	mgr.Register(task1)
	mgr.Register(task2)
	mgr.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	mgr.Stop()

	if task1.runCount.Load() < 2 {
		t.Errorf("task1 expected at least 2 runs, got %d", task1.runCount.Load())
	}
	if task2.runCount.Load() < 2 {
		t.Errorf("task2 expected at least 2 runs, got %d", task2.runCount.Load())
	}
}

func TestTriggerSyncUnknownTaskReturnsError(t *testing.T) {
	mgr := NewSyncManager()
	err := mgr.TriggerSync("nonexistent")
	if err == nil {
		t.Error("expected error for unknown task")
	}
}
