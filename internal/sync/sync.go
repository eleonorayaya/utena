package sync

import (
	"context"
	"fmt"
	"log/slog"
	stdsync "sync"
	"time"
)

type SyncTask interface {
	Name() string
	Interval() time.Duration
	Run(ctx context.Context) error
}

type SyncManager struct {
	tasks   map[string]SyncTask
	cancel  context.CancelFunc
	wg      stdsync.WaitGroup
	mu      stdsync.Mutex
	trigger map[string]chan struct{}
}

func NewSyncManager() *SyncManager {
	return &SyncManager{
		tasks:   make(map[string]SyncTask),
		trigger: make(map[string]chan struct{}),
	}
}

func (s *SyncManager) Register(task SyncTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.Name()] = task
	s.trigger[task.Name()] = make(chan struct{}, 1)
}

func (s *SyncManager) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, task := range s.tasks {
		s.wg.Add(1)
		go s.runLoop(ctx, task, s.trigger[task.Name()])
	}
}

func (s *SyncManager) runLoop(ctx context.Context, task SyncTask, trigger <-chan struct{}) {
	defer s.wg.Done()

	if err := task.Run(ctx); err != nil {
		slog.Error("sync task failed", "task", task.Name(), "error", err)
	}

	ticker := time.NewTicker(task.Interval())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := task.Run(ctx); err != nil {
				slog.Error("sync task failed", "task", task.Name(), "error", err)
			}
		case <-trigger:
			if err := task.Run(ctx); err != nil {
				slog.Error("sync task failed", "task", task.Name(), "error", err)
			}
		case <-ctx.Done():
			return
		}
	}
}

func (s *SyncManager) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *SyncManager) TriggerSync(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.trigger[name]
	if !ok {
		return fmt.Errorf("unknown sync task: %s", name)
	}
	select {
	case ch <- struct{}{}:
	default:
	}
	return nil
}
