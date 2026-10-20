package gossipsub

import (
	"context"
	"fmt"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu-sidecar/pkg/cache"
	"github.com/ethpandaops/xatu/pkg/proto/libp2p/gossipsub"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/google/uuid"
	ttlcache "github.com/jellydator/ttlcache/v3"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// DataColumnCompleted represents a processed custody data column completion.
type DataColumnCompleted struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawDataColumnCompleted
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
}

// RawDataColumnCompleted is the raw data column completion received from the client. PeerID is the
// peer whose message completed the column, empty when no peer did (EL, reconstruction).
type RawDataColumnCompleted struct {
	TimestampMs         int64  `json:"timestamp_ms"`
	PeerID              string `json:"peer_id"`
	Topic               string `json:"topic,omitempty"`
	BlockRoot           string `json:"block_root"`
	Slot                uint64 `json:"slot"`
	ColumnIndex         uint64 `json:"column_index"`
	CompletedVia        string `json:"completed_via"`
	CellsTotal          uint32 `json:"cells_total"`
	CellsFromGossip     uint32 `json:"cells_from_gossip"`
	CellsFromEL         uint32 `json:"cells_from_el"`
	ContributingPeers   uint32 `json:"contributing_peers"`
	KZGCommitmentsCount uint32 `json:"kzg_commitments_count"`
}

// NewDataColumnCompleted creates a DataColumnCompleted from raw event data and records its block slot in blockSlots.
func NewDataColumnCompleted(log logrus.FieldLogger, event *RawDataColumnCompleted, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], blockSlots *cache.BlockSlotCache, clientMeta *xatu.ClientMeta) *DataColumnCompleted {
	blockSlots.Resolve(event.BlockRoot, event.Slot)

	return &DataColumnCompleted{
		log:            log.WithField("event", "LIBP2P_TRACE_DATA_COLUMN_COMPLETED"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the completion event with additional metadata and returns a decorated event.
func (e *DataColumnCompleted) Decorate(_ context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := e.now.Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_DATA_COLUMN_COMPLETED,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceDataColumnCompleted{
			Libp2PTraceDataColumnCompleted: &gossipsub.DataColumnCompleted{
				BlockRoot:           wrapperspb.String(e.event.BlockRoot),
				ColumnIndex:         wrapperspb.UInt64(e.event.ColumnIndex),
				CompletedVia:        wrapperspb.String(e.event.CompletedVia),
				CellsTotal:          wrapperspb.UInt32(e.event.CellsTotal),
				CellsFromGossip:     wrapperspb.UInt32(e.event.CellsFromGossip),
				CellsFromEl:         wrapperspb.UInt32(e.event.CellsFromEL),
				ContributingPeers:   wrapperspb.UInt32(e.event.ContributingPeers),
				KzgCommitmentsCount: wrapperspb.UInt32(e.event.KZGCommitmentsCount),
			},
		},
	}

	timing, err := newBlockTiming(e.wallclock, e.now, e.event.Slot)
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra data column completed data")

		return decoratedEvent, nil
	}

	decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceDataColumnCompleted{
		Libp2PTraceDataColumnCompleted: &xatu.ClientMeta_AdditionalLibP2PTraceDataColumnCompletedData{
			WallclockSlot:  timing.WallclockSlot,
			WallclockEpoch: timing.WallclockEpoch,
			Slot:           timing.Slot,
			Epoch:          timing.Epoch,
			Propagation:    timing.Propagation,
			Metadata:       peerMetadata(e.event.PeerID),
			Topic:          optionalString(e.event.Topic),
		},
	}

	return decoratedEvent, nil
}

// ShouldIgnore rejects malformed completions and keeps only the first completion per block root and
// column index, dropping completions for blocks more than 16 slots behind the wallclock.
func (e *DataColumnCompleted) ShouldIgnore(_ context.Context) (bool, error) {
	if e.event == nil {
		return true, nil
	}

	allowed := []string{
		DataColumnCompletedViaFullGossip,
		DataColumnCompletedViaPartialGossip,
		DataColumnCompletedViaEL,
		DataColumnCompletedViaReconstruction,
		DataColumnCompletedViaUnknown,
	}
	if err := validateEnum(e.event.CompletedVia, allowed, ErrInvalidCompletedVia); err != nil {
		return true, err
	}

	key := fmt.Sprintf("%s:%d", e.event.BlockRoot, e.event.ColumnIndex)

	item, retrieved := e.duplicateCache.GetOrSet(key, e.now, ttlcache.WithTTL[string, time.Time](ttlcache.DefaultTTL))
	if retrieved {
		e.log.WithFields(logrus.Fields{
			logFieldTimeSinceFirstItem: time.Since(item.Value()),
			logFieldSlot:               e.event.Slot,
			logFieldColumnIndex:        e.event.ColumnIndex,
			"completed_via":            e.event.CompletedVia,
		}).Debug("Duplicate data column completion received")

		return true, nil
	}

	return isStaleSlot(e.wallclock, e.event.Slot)
}
