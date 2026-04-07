package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type Job interface {
	Name() string
	Interval() time.Duration
	Run(ctx context.Context) error
}

type JobService struct {
	jobs    map[string]Job
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	mu      sync.Mutex
	trigger map[string]chan struct{}
}

func NewJobService() *JobService {
	return &JobService{
		jobs:    make(map[string]Job),
		trigger: make(map[string]chan struct{}),
	}
}

func (s *JobService) Register(job Job) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs[job.Name()] = job
	s.trigger[job.Name()] = make(chan struct{}, 1)
}

func (s *JobService) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.jobs {
		s.wg.Add(1)
		go s.runLoop(ctx, job, s.trigger[job.Name()])
	}
}

func (s *JobService) runLoop(ctx context.Context, job Job, trigger <-chan struct{}) {
	defer s.wg.Done()

	slog.Info("running job", "job", job.Name(), "trigger", "startup")
	if err := job.Run(ctx); err != nil {
		slog.Error("job failed", "job", job.Name(), "error", err)
	}

	ticker := time.NewTicker(job.Interval())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			slog.Info("running job", "job", job.Name(), "trigger", "interval")
			if err := job.Run(ctx); err != nil {
				slog.Error("job failed", "job", job.Name(), "error", err)
			}
		case <-trigger:
			slog.Info("running job", "job", job.Name(), "trigger", "manual")
			if err := job.Run(ctx); err != nil {
				slog.Error("job failed", "job", job.Name(), "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *JobService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *JobService) TriggerJob(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.trigger[name]
	if !ok {
		return fmt.Errorf("unknown job: %s", name)
	}
	select {
	case ch <- struct{}{}:
	default:
	}
	return nil
}
