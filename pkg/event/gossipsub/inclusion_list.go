package gossipsub

import (
	"context"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu/pkg/proto/libp2p"
	"github.com/ethpandaops/xatu/pkg/proto/libp2p/gossipsub"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/google/uuid"
	ttlcache "github.com/jellydator/ttlcache/v3"
	hashstructure "github.com/mitchellh/hashstructure/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// InclusionList represents a processed EIP-7805 (FOCIL) inclusion_list event from gossipsub.
type InclusionList struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawInclusionList
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
}

// RawInclusionList represents the raw signed inclusion list data received from gossipsub.
// Transactions holds each opaque transaction of the list as 0x-prefixed hex, in list order.
type RawInclusionList struct {
	TimestampMs    int64    `json:"timestamp_ms"`
	Slot           uint64   `json:"slot"`
	ValidatorIndex uint64   `json:"validator_index"`
	MessageSize    uint32   `json:"message_size"`
	DependentRoot  string   `json:"dependent_root"`
	PeerID         string   `json:"peer_id"`
	MessageID      string   `json:"message_id"`
	Topic          string   `json:"topic"`
	Transactions   []string `json:"transactions"`
}

// NewInclusionList creates a new InclusionList instance from raw event data.
func NewInclusionList(log logrus.FieldLogger, event *RawInclusionList, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *InclusionList {
	return &InclusionList{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_INCLUSION_LIST"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the inclusion list event with additional metadata and returns a decorated event.
func (e *InclusionList) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	data, err := e.summarize()
	if err != nil {
		return nil, err
	}

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_INCLUSION_LIST,
			DateTime: timestamppb.New(time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubInclusionList{
			Libp2PTraceGossipsubInclusionList: data,
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra inclusion list data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubInclusionList{
			Libp2PTraceGossipsubInclusionList: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the inclusion list event should be ignored based on deduplication.
func (e *InclusionList) ShouldIgnore(_ context.Context) (bool, error) {
	if e.event == nil {
		return true, nil
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
			logFieldSlot:               e.event.Slot,
			logFieldValidatorIndex:     e.event.ValidatorIndex,
		}).Debug("Duplicate inclusion list event received")

		return true, nil
	}

	return false, nil
}

func (e *InclusionList) summarize() (*gossipsub.InclusionList, error) {
	var totalBytes uint64

	hashes := make([]string, 0, len(e.event.Transactions))

	for i, encoded := range e.event.Transactions {
		tx, err := hexutil.Decode(encoded)
		if err != nil {
			return nil, fmt.Errorf("failed to decode transaction %d: %w", i, err)
		}

		totalBytes += uint64(len(tx))
		hashes = append(hashes, crypto.Keccak256Hash(tx).Hex())
	}

	return &gossipsub.InclusionList{
		Slot:                   wrapperspb.UInt64(e.event.Slot),
		ValidatorIndex:         wrapperspb.UInt64(e.event.ValidatorIndex),
		DependentRoot:          wrapperspb.String(e.event.DependentRoot),
		TransactionsCount:      wrapperspb.UInt32(uint32(len(hashes))), //nolint:gosec // bounded by MAX_BYTES_PER_INCLUSION_LIST.
		TransactionsTotalBytes: wrapperspb.UInt64(totalBytes),
		TransactionHashes:      hashes,
	}, nil
}

func (e *InclusionList) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubInclusionListData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	slot := e.wallclock.Slots().FromNumber(e.event.Slot)
	epoch := e.wallclock.Epochs().FromSlot(e.event.Slot)

	slotStartDiff := timestamp.Add(e.clockDrift).Sub(slot.TimeWindow().Start()).Milliseconds()
	if slotStartDiff < 0 {
		slotStartDiff = 0
	}

	return &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubInclusionListData{
		WallclockSlot: &xatu.SlotV2{
			Number:        wrapperspb.UInt64(wallclockSlot.Number()),
			StartDateTime: timestamppb.New(wallclockSlot.TimeWindow().Start()),
		},
		WallclockEpoch: &xatu.EpochV2{
			Number:        wrapperspb.UInt64(wallclockEpoch.Number()),
			StartDateTime: timestamppb.New(wallclockEpoch.TimeWindow().Start()),
		},
		Epoch: &xatu.EpochV2{
			Number:        wrapperspb.UInt64(epoch.Number()),
			StartDateTime: timestamppb.New(epoch.TimeWindow().Start()),
		},
		Slot: &xatu.SlotV2{
			Number:        wrapperspb.UInt64(e.event.Slot),
			StartDateTime: timestamppb.New(slot.TimeWindow().Start()),
		},
		Propagation: &xatu.PropagationV2{
			SlotStartDiff: wrapperspb.UInt64(uint64(slotStartDiff)),
		},
		Metadata:    &libp2p.TraceEventMetadata{PeerId: wrapperspb.String(e.event.PeerID)},
		Topic:       wrapperspb.String(e.event.Topic),
		MessageSize: wrapperspb.UInt32(e.event.MessageSize),
		MessageId:   wrapperspb.String(e.event.MessageID),
	}, nil
}
