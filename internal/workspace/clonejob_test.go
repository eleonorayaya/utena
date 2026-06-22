package workspace

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProgressTracker_SetGetClear(t *testing.T) {
	tr := newProgressTracker()

	require.Empty(t, tr.get(1))

	tr.set(1, "Receiving objects: 42%")
	require.Equal(t, "Receiving objects: 42%", tr.get(1))

	tr.set(1, "Resolving deltas: 100%")
	require.Equal(t, "Resolving deltas: 100%", tr.get(1))

	tr.clear(1)
	require.Empty(t, tr.get(1))
}

func TestProgressTracker_IsolatesByID(t *testing.T) {
	tr := newProgressTracker()
	tr.set(1, "a")
	tr.set(2, "b")
	require.Equal(t, "a", tr.get(1))
	require.Equal(t, "b", tr.get(2))
	tr.clear(1)
	require.Empty(t, tr.get(1))
	require.Equal(t, "b", tr.get(2))
}
