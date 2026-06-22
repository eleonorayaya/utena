package workspace

import "sync"

// progressTracker holds the latest live progress line for an in-flight bare
// operation, keyed by workspace ID.
// ponytail: in-memory only; progress is ephemeral and updates many times/sec,
// so it is never persisted — only the Status/StatusError on the row are.
type progressTracker struct {
	mu sync.Mutex
	m  map[uint]string
}

func newProgressTracker() *progressTracker {
	return &progressTracker{m: make(map[uint]string)}
}

func (t *progressTracker) set(id uint, msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.m[id] = msg
}

func (t *progressTracker) get(id uint) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.m[id]
}

func (t *progressTracker) clear(id uint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.m, id)
}
