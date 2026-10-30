package drive

import (
	"container/list"
	"sync"
)

// lru keeps recent versions and content, bounded by an estimate of their
// size in bytes.
type lru struct {
	mu    sync.Mutex
	cap   int64
	size  int64
	order *list.List
	items map[string]*list.Element
}

type item struct {
	key  string
	val  any
	size int64
}

func newLRU(capacity int64) *lru {
	return &lru{cap: capacity, order: list.New(), items: map[string]*list.Element{}}
}

func (c *lru) get(key string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*item).val, true
}

func (c *lru) put(key string, val any, size int64) {
	if size > c.cap {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		c.size -= el.Value.(*item).size
		el.Value = &item{key: key, val: val, size: size}
		c.size += size
		c.order.MoveToFront(el)
	} else {
		c.items[key] = c.order.PushFront(&item{key: key, val: val, size: size})
		c.size += size
	}
	for c.size > c.cap {
		el := c.order.Back()
		it := el.Value.(*item)
		c.order.Remove(el)
		delete(c.items, it.key)
		c.size -= it.size
	}
}
