package jobs

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type mockJob struct {
	name     string
	interval time.Duration
	runCount atomic.Int32
	err      error
}

func (m *mockJob) Name() string            { return m.name }
func (m *mockJob) Interval() time.Duration { return m.interval }
func (m *mockJob) Run(_ context.Context) error {
	m.runCount.Add(1)
	return m.err
}

func TestJobRunsAtInterval(t *testing.T) {
	svc := NewJobService()
	job := &mockJob{name: "ticker", interval: 50 * time.Millisecond}
	svc.Register(job)
	svc.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	svc.Stop()

	count := job.runCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 runs, got %d", count)
	}
}

func TestStopCancelsRunningJobs(t *testing.T) {
	svc := NewJobService()
	job := &mockJob{name: "stopper", interval: 50 * time.Millisecond}
	svc.Register(job)
	svc.Start(context.Background())

	time.Sleep(100 * time.Millisecond)
	svc.Stop()

	countAfterStop := job.runCount.Load()
	time.Sleep(150 * time.Millisecond)
	countLater := job.runCount.Load()

	if countLater != countAfterStop {
		t.Errorf("job continued running after stop: %d -> %d", countAfterStop, countLater)
	}
}

func TestTriggerJobRunsImmediately(t *testing.T) {
	svc := NewJobService()
	job := &mockJob{name: "manual", interval: 10 * time.Second}
	svc.Register(job)
	svc.Start(context.Background())
	defer svc.Stop()

	time.Sleep(20 * time.Millisecond)
	initialCount := job.runCount.Load()

	if err := svc.TriggerJob("manual"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if job.runCount.Load() <= initialCount {
		t.Error("expected job to run after trigger")
	}
}

func TestJobErrorsDoNotCrashService(t *testing.T) {
	svc := NewJobService()
	failing := &mockJob{name: "failing", interval: 50 * time.Millisecond, err: errors.New("boom")}
	healthy := &mockJob{name: "healthy", interval: 50 * time.Millisecond}
	svc.Register(failing)
	svc.Register(healthy)
	svc.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	svc.Stop()

	if failing.runCount.Load() < 2 {
		t.Error("failing job should have continued running despite errors")
	}
	if healthy.runCount.Load() < 2 {
		t.Error("healthy job should have kept running")
	}
}

func TestMultipleJobsRunIndependently(t *testing.T) {
	svc := NewJobService()
	job1 := &mockJob{name: "job1", interval: 50 * time.Millisecond}
	job2 := &mockJob{name: "job2", interval: 50 * time.Millisecond}
	svc.Register(job1)
	svc.Register(job2)
	svc.Start(context.Background())

	time.Sleep(200 * time.Millisecond)
	svc.Stop()

	if job1.runCount.Load() < 2 {
		t.Errorf("job1 expected at least 2 runs, got %d", job1.runCount.Load())
	}
	if job2.runCount.Load() < 2 {
		t.Errorf("job2 expected at least 2 runs, got %d", job2.runCount.Load())
	}
}

func TestTriggerUnknownJobReturnsError(t *testing.T) {
	svc := NewJobService()
	err := svc.TriggerJob("nonexistent")
	if err == nil {
		t.Error("expected error for unknown job")
	}
}
