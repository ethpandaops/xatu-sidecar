// Package cache provides caching utilities for deduplication in the Xatu sidecar.
package cache

import (
	"time"

	"github.com/jellydator/ttlcache/v3"
)

// DuplicateCache manages TTL-based caches for deduplicating various event types.
type DuplicateCache struct {
	GossipsubBeaconBlock       *ttlcache.Cache[string, time.Time]
	GossipsubAggregateAndProof *ttlcache.Cache[string, time.Time]
	GossipsubAttestation       *ttlcache.Cache[string, time.Time]
	GossipsubBlobSidecar       *ttlcache.Cache[string, time.Time]
	GossipsubDataColumnSidecar *ttlcache.Cache[string, time.Time]
}

const (
	// ConsensusTTL defines the time-to-live for consensus-related cache entries.
	ConsensusTTL = 7 * time.Minute
)

// NewDuplicateCache creates a new instance of DuplicateCache with configured TTL caches.
func NewDuplicateCache() *DuplicateCache {
	return &DuplicateCache{
		GossipsubBeaconBlock: ttlcache.New(
			ttlcache.WithTTL[string, time.Time](ConsensusTTL),
		),
		GossipsubAggregateAndProof: ttlcache.New(
			ttlcache.WithTTL[string, time.Time](ConsensusTTL),
		),
		GossipsubAttestation: ttlcache.New(
			ttlcache.WithTTL[string, time.Time](ConsensusTTL),
		),
		GossipsubBlobSidecar: ttlcache.New(
			ttlcache.WithTTL[string, time.Time](ConsensusTTL),
		),
		GossipsubDataColumnSidecar: ttlcache.New(
			ttlcache.WithTTL[string, time.Time](ConsensusTTL),
		),
	}
}

// Start begins the background cleanup process for all caches.
func (d *DuplicateCache) Start() {
	go d.GossipsubBeaconBlock.Start()
	go d.GossipsubAggregateAndProof.Start()
	go d.GossipsubAttestation.Start()
	go d.GossipsubBlobSidecar.Start()
	go d.GossipsubDataColumnSidecar.Start()
}
