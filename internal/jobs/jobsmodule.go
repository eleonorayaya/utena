package jobs

import (
	"context"

	"github.com/go-chi/chi/v5"
)

type JobsModule struct {
	Service *JobService
}

func NewJobsModule() *JobsModule {
	return &JobsModule{
		Service: NewJobService(),
	}
}

func (m *JobsModule) OnAppStart(ctx context.Context) error {
	m.Service.Start(ctx)
	return nil
}

func (m *JobsModule) OnAppEnd(ctx context.Context) error {
	m.Service.Stop()
	return nil
}

func (m *JobsModule) Routes() chi.Router {
	return chi.NewRouter()
}
