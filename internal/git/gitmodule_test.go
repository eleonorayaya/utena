package git

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	usync "github.com/eleonorayaya/utena/internal/sync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGitModule_Models(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	models := module.Models()
	assert.Len(t, models, 4)
}

func TestGitModule_RegisterSyncTasks(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	manager := usync.NewSyncManager()
	module.RegisterSyncTasks(manager)

	assert.NoError(t, manager.TriggerSync("git.prs"))
	assert.NoError(t, manager.TriggerSync("git.branches"))
	assert.Error(t, manager.TriggerSync("nonexistent"))
}

func TestGitModule_InitializesWithoutError(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	service := NewGitService(database, WithEventBus(bus))
	module := &GitModule{Service: service}

	require.NotNil(t, module.Service)
}
