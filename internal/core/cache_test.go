package core

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/otyang/humanblnk/money/currency"
)

var usd = currency.MustGet(currency.USD)

func TestCacheKeepsTheMostRecentlyUsed(t *testing.T) {
	c := newCache(2)
	c.put("a", accountInfo{currency: usd})
	c.put("b", accountInfo{currency: usd, system: true})
	if _, ok := c.get("a"); !ok { // a is now the most recent
		t.Fatal("a missing")
	}
	c.put("c", accountInfo{currency: usd})
	if _, ok := c.get("b"); ok {
		t.Fatal("b, the least recently used, should have gone")
	}
	for _, id := range []string{"a", "c"} {
		if _, ok := c.get(id); !ok {
			t.Fatalf("%s missing", id)
		}
	}
	c.put("c", accountInfo{currency: usd, system: true}) // an update, not a new entry
	if info, _ := c.get("c"); !info.system || c.len() != 2 {
		t.Fatalf("after update: %+v, %d entries", info, c.len())
	}
}

func TestCacheDefaultSize(t *testing.T) {
	if c := newCache(0); c.size != DefaultCacheSize {
		t.Fatalf("size %d", c.size)
	}
}

// A long-running service touches far more accounts than it keeps: memory
// must stop growing at the cache's size, not grow with every account.
func TestCacheMemoryIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("touches a million accounts")
	}
	const size, touched = 1_000, 1_000_000
	c := newCache(size)
	heap := func() uint64 {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return m.HeapAlloc
	}
	for i := range size {
		c.put(fmt.Sprintf("bln_%d", i), accountInfo{currency: usd})
	}
	before := heap()
	for i := size; i < touched; i++ {
		c.put(fmt.Sprintf("bln_%d", i), accountInfo{currency: usd})
	}
	after := heap()
	if c.len() != size {
		t.Fatalf("%d entries after %d accounts, want %d", c.len(), touched, size)
	}
	// Unbounded, a million entries would add tens of megabytes.
	if after > before && after-before > 4<<20 {
		t.Fatalf("heap grew %d KiB after touching %d accounts with a %d-entry cache", (after-before)>>10, touched, size)
	}
}

func TestCacheConcurrent(t *testing.T) {
	c := newCache(100)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 1000 {
				id := fmt.Sprintf("bln_%d", (g*1000+i)%300)
				c.put(id, accountInfo{currency: usd})
				c.get(id)
			}
		}()
	}
	wg.Wait()
	if c.len() > 100 {
		t.Fatalf("%d entries, want at most 100", c.len())
	}
}
