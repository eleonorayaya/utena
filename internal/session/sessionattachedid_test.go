package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStore_AttachedID(t *testing.T) {
	store, _, _ := setupSessionStore(t)

	id, err := store.AttachedID()
	require.NoError(t, err)
	assert.Zero(t, id)

	require.NoError(t, store.Add(&Session{Name: "detached", Status: StatusActive}))
	attached := &Session{Name: "attached", Status: StatusActive, IsAttached: true}
	require.NoError(t, store.Add(attached))

	id, err = store.AttachedID()
	require.NoError(t, err)
	assert.Equal(t, attached.ID, id)
}

func TestSessionService_AttachedSessionID(t *testing.T) {
	service, store, _, _, _, _ := setupSessionService(t)

	id, err := service.AttachedSessionID(context.Background())
	require.NoError(t, err)
	assert.Zero(t, id)

	attached := &Session{Name: "attached", Status: StatusActive, IsAttached: true}
	require.NoError(t, store.Add(attached))

	id, err = service.AttachedSessionID(context.Background())
	require.NoError(t, err)
	assert.Equal(t, attached.ID, id)
}
