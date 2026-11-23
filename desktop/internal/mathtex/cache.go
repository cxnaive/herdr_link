package mathtex

import (
	"context"
	"sync"
)

const cacheLimit = 128
const cacheBytes = 16 * 1024 * 1024

type key struct {
	source  string
	display bool
}
type result struct {
	formula *Formula
	ready   bool
}

// Cache owns one background renderer and a bounded, memory-only cache. Failures
// are cached as well; scrolling never retries a malformed formula every frame.
type Cache struct {
	mu       sync.Mutex
	ctx      context.Context
	queue    chan key
	entries  map[key]result
	order    []key
	revision uint64
	pending  int
	bytes    int
}

func NewCache(ctx context.Context) *Cache {
	c := &Cache{ctx: ctx, queue: make(chan key, 32), entries: make(map[key]result)}
	go c.run()
	return c
}
func (c *Cache) Get(source string, display bool) (*Formula, bool) {
	if len(source) > MaxSource {
		return nil, true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	k := key{source, display}
	if value, ok := c.entries[k]; ok {
		return value.formula, value.ready
	}
	if c.ctx.Err() != nil {
		return nil, true
	}
	select {
	case c.queue <- k:
		c.entries[k] = result{}
		c.pending++
	default:
	}
	return nil, false
}
func (c *Cache) State() (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revision, c.pending > 0
}
func (c *Cache) run() {
	var renderer Renderer
	for {
		select {
		case <-c.ctx.Done():
			return
		case k := <-c.queue:
			formula, _ := renderer.Render(c.ctx, k.source, k.display)
			c.mu.Lock()
			c.entries[k] = result{formula, true}
			c.order = append(c.order, k)
			if formula != nil {
				c.bytes += len(formula.SVG)
			}
			for len(c.order) > cacheLimit || c.bytes > cacheBytes {
				if old := c.entries[c.order[0]].formula; old != nil {
					c.bytes -= len(old.SVG)
				}
				delete(c.entries, c.order[0])
				c.order = c.order[1:]
			}
			c.pending--
			c.revision++
			c.mu.Unlock()
		}
	}
}
