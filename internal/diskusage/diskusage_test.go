package diskusage

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMeasure struct {
	mu    sync.Mutex
	sizes map[string]int64
	errs  map[string]error
	calls int
}

func (f *fakeMeasure) measure(root string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.sizes[root], f.errs[root]
}

func (f *fakeMeasure) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func waitDrained(t *testing.T, c *Cache) {
	t.Helper()
	c.mu.Lock()
	done := c.done
	c.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("disk usage refresh did not finish")
	}
}

func TestCache_MeasuresInBackgroundAndCaches(t *testing.T) {
	fake := &fakeMeasure{sizes: map[string]int64{"/a": 100, "/b": 200}}
	c := NewCache(fake.measure, time.Minute)

	assert.Empty(t, c.Sizes([]string{"/a", "/b", ""}))
	waitDrained(t, c)

	assert.Equal(t, map[string]int64{"/a": 100, "/b": 200}, c.Sizes([]string{"/a", "/b"}))
	waitDrained(t, c)
	assert.Equal(t, 2, fake.callCount())
}

func TestCache_RefreshesAfterTTL(t *testing.T) {
	fake := &fakeMeasure{sizes: map[string]int64{"/a": 100}}
	c := NewCache(fake.measure, time.Minute)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	c.Sizes([]string{"/a"})
	waitDrained(t, c)

	fake.mu.Lock()
	fake.sizes["/a"] = 300
	fake.mu.Unlock()
	now = now.Add(2 * time.Minute)

	assert.Equal(t, map[string]int64{"/a": 100}, c.Sizes([]string{"/a"}))
	waitDrained(t, c)
	assert.Equal(t, map[string]int64{"/a": 300}, c.Sizes([]string{"/a"}))
}

func TestCache_SlowWalksStretchTheRefreshInterval(t *testing.T) {
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	clock := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	advance := func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(d)
	}
	calls := 0
	c := NewCache(func(string) (int64, error) {
		calls++
		advance(time.Minute)
		return 1, nil
	}, time.Minute)
	c.now = clock

	c.Sizes([]string{"/a"})
	waitDrained(t, c)
	advance(5 * time.Minute)
	c.Sizes([]string{"/a"})
	waitDrained(t, c)
	assert.Equal(t, 1, calls)

	advance(5 * time.Minute)
	c.Sizes([]string{"/a"})
	waitDrained(t, c)
	assert.Equal(t, 2, calls)
}

func TestCache_ErrorLeavesSizeUnknownUntilTTL(t *testing.T) {
	fake := &fakeMeasure{errs: map[string]error{"/a": errors.New("gone")}}
	c := NewCache(fake.measure, time.Minute)

	c.Sizes([]string{"/a"})
	waitDrained(t, c)
	assert.Empty(t, c.Sizes([]string{"/a"}))
	waitDrained(t, c)
	assert.Equal(t, 1, fake.callCount())
}

func TestCache_PrunesRootsNoLongerRequested(t *testing.T) {
	fake := &fakeMeasure{sizes: map[string]int64{"/a": 1, "/b": 2}}
	c := NewCache(fake.measure, time.Minute)

	c.Sizes([]string{"/a", "/b"})
	waitDrained(t, c)
	c.Sizes([]string{"/a"})

	c.mu.Lock()
	defer c.mu.Unlock()
	assert.Contains(t, c.entries, "/a")
	assert.NotContains(t, c.entries, "/b")
}

func TestDirSize_SumsFilesAndSkipsSymlinkTargets(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a"), make([]byte, 64<<10), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "sub", "b"), make([]byte, 64<<10), 0o644))
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "big"), make([]byte, 1<<20), 0o644))
	require.NoError(t, os.Symlink(outside, filepath.Join(root, "link")))

	size, err := DirSize(root)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, size, int64(128<<10))
	assert.Less(t, size, int64(1<<20))
}

func TestDirSize_MissingRootErrors(t *testing.T) {
	_, err := DirSize(filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
