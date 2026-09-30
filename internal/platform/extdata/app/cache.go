package app

import (
	"container/list"
	"sync"
	"time"
)

// DefaultCacheEntries là số mục tối đa của bộ nhớ đệm trong tiến trình.
const DefaultCacheEntries = 4096

// Entry là một mục đệm theo ô lưới / tile (không gắn người gọi).
type Entry struct {
	Data        []byte
	ContentType string
	FetchedAt   time.Time
	Expires     time.Time
}

// Cache là bộ nhớ đệm LRU có hạn dùng. Khóa chỉ gồm nguồn + ô/tile, nên nhiều người
// cùng ô dùng chung một lần gọi ra ngoài (giảm lưu lượng và dấu vết truy vấn).
type Cache struct {
	mu    sync.Mutex
	max   int
	ll    *list.List
	items map[string]*list.Element
}

type cacheItem struct {
	key string
	e   Entry
}

// NewCache dựng bộ nhớ đệm; max ≤ 0 → DefaultCacheEntries.
func NewCache(max int) *Cache {
	if max <= 0 {
		max = DefaultCacheEntries
	}
	return &Cache{max: max, ll: list.New(), items: map[string]*list.Element{}}
}

// Get trả mục còn hạn tại now.
func (c *Cache) Get(key string, now time.Time) (Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return Entry{}, false
	}
	it := el.Value.(*cacheItem)
	if !now.Before(it.e.Expires) {
		c.ll.Remove(el)
		delete(c.items, key)
		return Entry{}, false
	}
	c.ll.MoveToFront(el)
	return it.e, true
}

// Put ghi mục, bỏ mục ít dùng nhất khi đầy.
func (c *Cache) Put(key string, e Entry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[key]; ok {
		el.Value.(*cacheItem).e = e
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&cacheItem{key: key, e: e})
	for c.ll.Len() > c.max {
		last := c.ll.Back()
		c.ll.Remove(last)
		delete(c.items, last.Value.(*cacheItem).key)
	}
}

// Len trả số mục.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ll.Len()
}
