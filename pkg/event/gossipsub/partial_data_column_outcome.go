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
	hashstructure "github.com/mitchellh/hashstructure/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// PartialDataColumnOutcome represents a processed validation verdict for one received partial data column message.
type PartialDataColumnOutcome struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawPartialDataColumnOutcome
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
	slot           uint64
}

// RawPartialDataColumnOutcome is the raw partial data column validation verdict received from the
// client. PeerID is the sender of the partial. Slot is zero when unknown. NewCellsCount is the number
// of cells the message added to the node's copy of the column.
type RawPartialDataColumnOutcome struct {
	TimestampMs   int64  `json:"timestamp_ms"`
	PeerID        string `json:"peer_id"`
	Topic         string `json:"topic"`
	GroupID       string `json:"group_id"`
	BlockRoot     string `json:"block_root"`
	Slot          uint64 `json:"slot"`
	ColumnIndex   uint64 `json:"column_index"`
	Stage         string `json:"stage"`
	Outcome       string `json:"outcome"`
	Reason        string `json:"reason"`
	CellsCount    uint32 `json:"cells_count"`
	NewCellsCount uint32 `json:"new_cells_count"`
}

// NewPartialDataColumnOutcome creates a PartialDataColumnOutcome from raw event data. An unknown
// slot is resolved from blockSlots, and a known one is recorded there.
func NewPartialDataColumnOutcome(log logrus.FieldLogger, event *RawPartialDataColumnOutcome, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], blockSlots *cache.BlockSlotCache, clientMeta *xatu.ClientMeta) *PartialDataColumnOutcome {
	return &PartialDataColumnOutcome{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_PARTIAL_DATA_COLUMN_OUTCOME"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
		slot:           blockSlots.Resolve(event.BlockRoot, event.Slot),
	}
}

// Decorate enriches the outcome event with additional metadata and returns a decorated event.
func (e *PartialDataColumnOutcome) Decorate(_ context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := e.now.Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_PARTIAL_DATA_COLUMN_OUTCOME,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubPartialDataColumnOutcome{
			Libp2PTraceGossipsubPartialDataColumnOutcome: &gossipsub.PartialDataColumnOutcome{
				GroupId:       optionalString(e.event.GroupID),
				BlockRoot:     optionalString(e.event.BlockRoot),
				ColumnIndex:   wrapperspb.UInt64(e.event.ColumnIndex),
				Stage:         wrapperspb.String(e.event.Stage),
				Outcome:       wrapperspb.String(e.event.Outcome),
				Reason:        optionalString(e.event.Reason),
				CellsCount:    wrapperspb.UInt32(e.event.CellsCount),
				NewCellsCount: wrapperspb.UInt32(e.event.NewCellsCount),
			},
		},
	}

	timing, err := newBlockTiming(e.wallclock, e.now, e.slot)
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra partial data column outcome data")

		return decoratedEvent, nil
	}

	decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubPartialDataColumnOutcome{
		Libp2PTraceGossipsubPartialDataColumnOutcome: &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubPartialDataColumnOutcomeData{
			WallclockSlot:  timing.WallclockSlot,
			WallclockEpoch: timing.WallclockEpoch,
			Slot:           timing.Slot,
			Epoch:          timing.Epoch,
			Metadata:       peerMetadata(e.event.PeerID),
			Topic:          optionalString(e.event.Topic),
		},
	}

	return decoratedEvent, nil
}

// ShouldIgnore rejects malformed verdicts and drops exact duplicates and verdicts for blocks more
// than 16 slots behind the wallclock.
func (e *PartialDataColumnOutcome) ShouldIgnore(_ context.Context) (bool, error) {
	if e.event == nil {
		return true, nil
	}

	stages := []string{PartialOutcomeStageDecode, PartialOutcomeStageHeader, PartialOutcomeStageCells}
	if err := validateEnum(e.event.Stage, stages, ErrInvalidPartialStage); err != nil {
		return true, err
	}

	outcomes := []string{PartialOutcomeAccept, PartialOutcomeIgnore, PartialOutcomeReject}
	if err := validateEnum(e.event.Outcome, outcomes, ErrInvalidPartialOutcome); err != nil {
		return true, err
	}

	hash, err := hashstructure.Hash(e.event, hashstructure.FormatV2, nil)
	if err != nil {
		return true, err
	}

	item, retrieved := e.duplicateCache.GetOrSet(fmt.Sprint(hash), e.now, ttlcache.WithTTL[string, time.Time](ttlcache.DefaultTTL))
	if retrieved {
		e.log.WithFields(logrus.Fields{
			logFieldHash:               hash,
			logFieldTimeSinceFirstItem: time.Since(item.Value()),
			logFieldSlot:               e.slot,
			logFieldColumnIndex:        e.event.ColumnIndex,
		}).Debug("Duplicate partial data column outcome received")

		return true, nil
	}

	return isStaleSlot(e.wallclock, e.slot)
}
