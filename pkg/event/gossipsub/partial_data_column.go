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

// PartialDataColumn represents one processed gossipsub partial messages extension part on a data column topic.
type PartialDataColumn struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawPartialDataColumn
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
	slot           uint64
}

// RawPartialDataColumn is the raw partial data column part received from the client.
// PeerID is the remote peer: the sender for RECV, the recipient for SEND and DROP (empty when the
// client publishes to the router rather than to a peer). Slot is zero when unknown. Bitmaps are hex
// encoded SSZ bitlists. MessageSize is the encoded partial message size, zero for metadata-only parts.
type RawPartialDataColumn struct {
	TimestampMs            int64  `json:"timestamp_ms"`
	Direction              string `json:"direction"`
	CapturePoint           string `json:"capture_point"`
	PeerID                 string `json:"peer_id"`
	Topic                  string `json:"topic"`
	GroupID                string `json:"group_id"`
	GroupVersion           uint32 `json:"group_version"`
	BlockRoot              string `json:"block_root"`
	Slot                   uint64 `json:"slot"`
	ColumnIndex            uint64 `json:"column_index"`
	HasMessage             bool   `json:"has_message"`
	MessageSize            uint32 `json:"message_size"`
	CellsCount             uint32 `json:"cells_count"`
	CellsBitmap            string `json:"cells_bitmap"`
	BitmapLength           uint32 `json:"bitmap_length"`
	HasHeader              bool   `json:"has_header"`
	KZGCommitmentsCount    uint32 `json:"kzg_commitments_count"`
	ProposerIndex          uint64 `json:"proposer_index"`
	HasMetadata            bool   `json:"has_metadata"`
	MetadataSize           uint32 `json:"metadata_size"`
	MetadataAvailableCount uint32 `json:"metadata_available_count"`
	MetadataRequestsCount  uint32 `json:"metadata_requests_count"`
	MetadataAvailable      string `json:"metadata_available"`
	MetadataRequests       string `json:"metadata_requests"`
}

// NewPartialDataColumn creates a PartialDataColumn from raw event data. An unknown slot is
// resolved from blockSlots, and a known one is recorded there.
func NewPartialDataColumn(log logrus.FieldLogger, event *RawPartialDataColumn, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], blockSlots *cache.BlockSlotCache, clientMeta *xatu.ClientMeta) *PartialDataColumn {
	return &PartialDataColumn{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_PARTIAL_DATA_COLUMN"),
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

// Decorate enriches the partial data column event with additional metadata and returns a decorated event.
func (e *PartialDataColumn) Decorate(_ context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := e.now.Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_PARTIAL_DATA_COLUMN,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubPartialDataColumn{
			Libp2PTraceGossipsubPartialDataColumn: &gossipsub.PartialDataColumn{
				Direction:              wrapperspb.String(e.event.Direction),
				CapturePoint:           wrapperspb.String(e.event.CapturePoint),
				GroupId:                optionalString(e.event.GroupID),
				GroupVersion:           wrapperspb.UInt32(e.event.GroupVersion),
				BlockRoot:              optionalString(e.event.BlockRoot),
				ColumnIndex:            wrapperspb.UInt64(e.event.ColumnIndex),
				HasMessage:             wrapperspb.Bool(e.event.HasMessage),
				CellsCount:             wrapperspb.UInt32(e.event.CellsCount),
				CellsBitmap:            optionalString(e.event.CellsBitmap),
				BitmapLength:           wrapperspb.UInt32(e.event.BitmapLength),
				HasHeader:              wrapperspb.Bool(e.event.HasHeader),
				KzgCommitmentsCount:    wrapperspb.UInt32(e.event.KZGCommitmentsCount),
				ProposerIndex:          wrapperspb.UInt64(e.event.ProposerIndex),
				HasMetadata:            wrapperspb.Bool(e.event.HasMetadata),
				MetadataSize:           wrapperspb.UInt32(e.event.MetadataSize),
				MetadataAvailableCount: wrapperspb.UInt32(e.event.MetadataAvailableCount),
				MetadataRequestsCount:  wrapperspb.UInt32(e.event.MetadataRequestsCount),
				MetadataAvailable:      optionalString(e.event.MetadataAvailable),
				MetadataRequests:       optionalString(e.event.MetadataRequests),
			},
		},
	}

	timing, err := newBlockTiming(e.wallclock, e.now, e.slot)
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra partial data column data")

		return decoratedEvent, nil
	}

	decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubPartialDataColumn{
		Libp2PTraceGossipsubPartialDataColumn: &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubPartialDataColumnData{
			WallclockSlot:  timing.WallclockSlot,
			WallclockEpoch: timing.WallclockEpoch,
			Slot:           timing.Slot,
			Epoch:          timing.Epoch,
			Propagation:    timing.Propagation,
			Metadata:       peerMetadata(e.event.PeerID),
			Topic:          optionalString(e.event.Topic),
			MessageSize:    wrapperspb.UInt32(e.event.MessageSize),
		},
	}

	return decoratedEvent, nil
}

// ShouldIgnore rejects malformed parts and drops exact duplicates and parts for blocks more than
// 16 slots behind the wallclock.
func (e *PartialDataColumn) ShouldIgnore(_ context.Context) (bool, error) {
	if e.event == nil {
		return true, nil
	}

	if err := validateEnum(e.event.Direction, []string{PartialDirectionRecv, PartialDirectionSend, PartialDirectionDrop}, ErrInvalidPartialDirection); err != nil {
		return true, err
	}

	if err := validateEnum(e.event.CapturePoint, []string{PartialCapturePointWire, PartialCapturePointApp}, ErrInvalidPartialCapturePoint); err != nil {
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
		}).Debug("Duplicate partial data column event received")

		return true, nil
	}

	return isStaleSlot(e.wallclock, e.slot)
}
