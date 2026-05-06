package git

import (
	"context"
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/eleonorayaya/utena/internal/jobs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitModule_Models(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	models := module.Models()
	assert.Len(t, models, 4)
}

func TestGitModule_RegisterJobs(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	svc := jobs.NewJobService()
	module.RegisterJobs(svc)

	assert.NoError(t, svc.TriggerJob("git.prs"))
	assert.NoError(t, svc.TriggerJob("git.branches"))
	assert.Error(t, svc.TriggerJob("nonexistent"))
}

func TestGitModule_OnAppStart_SetsCurrentUser(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})

	bus := &mockEventBus{}
	module := NewGitModule(database, bus)
	module.Service.githubClient = &mockGitHubClient{currentUser: "testuser"}

	err = module.OnAppStart(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "testuser", module.Service.currentUser)
}

func TestGitModule_InitializesWithoutError(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := database.Close(); err != nil {
			t.Logf("close database: %v", err)
		}
	})

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	require.NotNil(t, module.Service)
}
