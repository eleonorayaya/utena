package tmux

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func TestTmuxModule_Models(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	mock := NewMockRunner()
	store := NewTmuxStore(database)
	module := NewTmuxModuleWithRunner(mock, store, bus)

	models := module.Models()
	require.Len(t, models, 1)
}

func TestTmuxModule_InitializesWithoutError(t *testing.T) {
	database, err := db.OpenInMemory()
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	bus := eventbus.NewEventBus()
	mock := NewMockRunner()
	store := NewTmuxStore(database)
	module := NewTmuxModuleWithRunner(mock, store, bus)

	require.NotNil(t, module.Service)
	require.NotNil(t, module.Controller)
	require.NotNil(t, module.Router)
}
