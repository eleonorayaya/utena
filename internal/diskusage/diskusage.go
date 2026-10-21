package diskusage

import (
	"io/fs"
	"log/slog"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

const (
	DefaultTTL     = 60 * time.Second
	walkCostFactor = 10
)

type entry struct {
	bytes   int64
	known   bool
	pending bool
	staleAt time.Time
}

type Cache struct {
	measure func(root string) (int64, error)
	ttl     time.Duration
	now     func() time.Time

	mu      sync.Mutex
	entries map[string]entry
	queue   []string
	done    chan struct{}
}

func NewCache(measure func(root string) (int64, error), ttl time.Duration) *Cache {
	return &Cache{
		measure: measure,
		ttl:     ttl,
		now:     time.Now,
		entries: make(map[string]entry),
	}
}

func (c *Cache) Sizes(roots []string) map[string]int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	wanted := make(map[string]struct{}, len(roots))
	sizes := make(map[string]int64, len(roots))
	now := c.now()
	for _, root := range roots {
		if root == "" {
			continue
		}
		wanted[root] = struct{}{}
		e := c.entries[root]
		if e.known {
			sizes[root] = e.bytes
		}
		if !e.pending && !now.Before(e.staleAt) {
			e.pending = true
			c.entries[root] = e
			c.queue = append(c.queue, root)
		}
	}
	for root, e := range c.entries {
		if _, ok := wanted[root]; !ok && !e.pending {
			delete(c.entries, root)
		}
	}
	if len(c.queue) > 0 && c.done == nil {
		c.done = make(chan struct{})
		go c.drain()
	}
	return sizes
}

func (c *Cache) drain() {
	for {
		c.mu.Lock()
		if len(c.queue) == 0 {
			close(c.done)
			c.done = nil
			c.mu.Unlock()
			return
		}
		root := c.queue[0]
		c.queue = c.queue[1:]
		c.mu.Unlock()

		start := c.now()
		bytes, err := c.measure(root)

		c.mu.Lock()
		now := c.now()
		e := c.entries[root]
		e.pending = false
		e.staleAt = now.Add(max(c.ttl, walkCostFactor*now.Sub(start)))
		if err != nil {
			slog.Warn("failed to measure disk usage", "root", root, "error", err)
		} else {
			e.bytes, e.known = bytes, true
		}
		c.entries[root] = e
		c.mu.Unlock()
	}
}

func DirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if st, ok := info.Sys().(*syscall.Stat_t); ok {
			total += st.Blocks * 512
		} else {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
