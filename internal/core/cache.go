package core

import (
	"container/list"
	"sync"

	"github.com/otyang/humanblnk/money/currency"
)

// DefaultCacheSize is how many accounts a Core remembers unless told
// otherwise: enough for a busy service's working set, at about 200 bytes
// each (some 20 MiB in all).
const DefaultCacheSize = 100_000

// accountInfo is what never changes about an account, so it can be cached:
// its currency, and whether it is a system account.
type accountInfo struct {
	currency currency.Currency
	system   bool
}

// cache remembers accountInfo for the most recently used accounts, at most
// size of them. A service that touches millions of accounts over its life
// would otherwise keep every one; the least recently used are dropped and
// read from Blnk again if needed.
type cache struct {
	mu    sync.Mutex
	size  int
	order *list.List               // front is the most recently used
	items map[string]*list.Element // account id -> element holding a cacheEntry
}

type cacheEntry struct {
	id   string
	info accountInfo
}

func newCache(size int) *cache {
	if size <= 0 {
		size = DefaultCacheSize
	}
	return &cache{size: size, order: list.New(), items: map[string]*list.Element{}}
}

func (c *cache) get(id string) (accountInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[id]
	if !ok {
		return accountInfo{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*cacheEntry).info, true
}

func (c *cache) put(id string, info accountInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[id]; ok {
		el.Value.(*cacheEntry).info = info
		c.order.MoveToFront(el)
		return
	}
	c.items[id] = c.order.PushFront(&cacheEntry{id: id, info: info})
	for c.order.Len() > c.size {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).id)
	}
}

func (c *cache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
