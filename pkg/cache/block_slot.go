package cache

import (
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// BlockSlotTTL bounds how long a block root to slot mapping is remembered.
const BlockSlotTTL = 7 * time.Minute

// BlockSlotCache maps block roots to slots learned from events that carry both,
// so events that only carry a block root (fulu partial data column groups) can be
// placed in a slot.
type BlockSlotCache struct {
	cache *ttlcache.Cache[string, uint64]
}

// NewBlockSlotCache creates an empty BlockSlotCache.
func NewBlockSlotCache() *BlockSlotCache {
	return &BlockSlotCache{
		cache: ttlcache.New(
			ttlcache.WithTTL[string, uint64](BlockSlotTTL),
			ttlcache.WithDisableTouchOnHit[string, uint64](),
		),
	}
}

// Start runs the background expiry loop until Stop is called.
func (c *BlockSlotCache) Start() {
	go c.cache.Start()
}

// Stop ends the background expiry loop.
func (c *BlockSlotCache) Stop() {
	c.cache.Stop()
}

// Resolve records slot for blockRoot when slot is known (non-zero) and returns it.
// When slot is zero it returns the slot previously recorded for blockRoot, or zero.
func (c *BlockSlotCache) Resolve(blockRoot string, slot uint64) uint64 {
	if blockRoot == "" {
		return slot
	}

	if slot != 0 {
		c.cache.Set(blockRoot, slot, ttlcache.DefaultTTL)

		return slot
	}

	if item := c.cache.Get(blockRoot); item != nil {
		return item.Value()
	}

	return 0
}
