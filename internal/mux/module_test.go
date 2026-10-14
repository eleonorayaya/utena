package mux

import (
	"testing"

	"github.com/eleonorayaya/utena/internal/db/testdb"
	"github.com/eleonorayaya/utena/internal/eventbus"
	"github.com/stretchr/testify/require"
)

func TestModule_Models(t *testing.T) {
	database := testdb.New(t)

	bus := eventbus.NewEventBus()
	mock := NewFakeDriver()
	module := NewModule(mock, bus, database)

	models := module.Models()
	require.Len(t, models, 1)
}

func TestModule_InitializesWithoutError(t *testing.T) {
	database := testdb.New(t)

	bus := eventbus.NewEventBus()
	mock := NewFakeDriver()
	module := NewModule(mock, bus, database)

	require.NotNil(t, module.Service)
	require.NotNil(t, module.Controller)
	require.NotNil(t, module.Router)
}
