// Package gossipsub provides Ethereum beacon chain event processing for gossipsub messages.
package gossipsub

import (
	"context"
	"fmt"
	"time"

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

// BeaconBlock represents a processed beacon block event from gossipsub.
type BeaconBlock struct {
	log logrus.FieldLogger

	now time.Time

	event          *RawBeaconBlock
	clockDrift     time.Duration
	wallclock      *ethwallclock.EthereumBeaconChain
	duplicateCache *ttlcache.Cache[string, time.Time]
	clientMeta     *xatu.ClientMeta
	id             uuid.UUID
}

// RawBeaconBlock represents the raw beacon block data received from gossipsub.
type RawBeaconBlock struct {
	PeerID        string `json:"peer_id"`
	MessageID     string `json:"message_id"`
	Topic         string `json:"topic"`
	MessageSize   uint32 `json:"message_size"`
	TimestampMs   int64  `json:"timestamp_ms"`
	Slot          uint64 `json:"slot"`
	Epoch         uint64 `json:"epoch"`
	BlockRoot     string `json:"block_root"`
	ProposerIndex uint64 `json:"proposer_index"`
}

// NewBeaconBlock creates a new BeaconBlock instance from raw event data.
func NewBeaconBlock(log logrus.FieldLogger, event *RawBeaconBlock, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *BeaconBlock {
	return &BeaconBlock{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_BEACON_BLOCK"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the beacon block event with additional metadata and returns a decorated event.
func (e *BeaconBlock) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_BEACON_BLOCK,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubBeaconBlock{
			Libp2PTraceGossipsubBeaconBlock: &gossipsub.BeaconBlock{
				Slot:          &wrapperspb.UInt64Value{Value: e.event.Slot},
				Block:         &wrapperspb.StringValue{Value: e.event.BlockRoot},
				ProposerIndex: &wrapperspb.UInt64Value{Value: e.event.ProposerIndex},
			},
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra beacon block data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubBeaconBlock{
			Libp2PTraceGossipsubBeaconBlock: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the beacon block event should be ignored based on deduplication and age.
func (e *BeaconBlock) ShouldIgnore(_ context.Context) (bool, error) {
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
			"hash":                  hash,
			"time_since_first_item": time.Since(item.Value()),
			"slot":                  e.event.Slot,
		}).Debug("Duplicate beacon block event received")

		return true, nil
	}

	currentSlot, _, err := e.wallclock.Now()
	if err != nil {
		return true, err
	}

	// ignore blocks that are more than 16 slots old
	slotLimit := currentSlot.Number() - 16

	if e.event.Slot < slotLimit {
		return true, nil
	}

	return false, nil
}

func (e *BeaconBlock) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconBlockData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconBlockData{
		WallclockSlot: &xatu.SlotV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockSlot.Number()},
			StartDateTime: timestamppb.New(wallclockSlot.TimeWindow().Start()),
		},
		WallclockEpoch: &xatu.EpochV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockEpoch.Number()},
			StartDateTime: timestamppb.New(wallclockEpoch.TimeWindow().Start()),
		},
	}

	slot := e.wallclock.Slots().FromNumber(e.event.Slot)
	epoch := e.wallclock.Epochs().FromSlot(e.event.Slot)

	extra.Slot = &xatu.SlotV2{
		StartDateTime: timestamppb.New(slot.TimeWindow().Start()),
		Number:        &wrapperspb.UInt64Value{Value: e.event.Slot},
	}

	extra.Epoch = &xatu.EpochV2{
		Number:        &wrapperspb.UInt64Value{Value: epoch.Number()},
		StartDateTime: timestamppb.New(epoch.TimeWindow().Start()),
	}

	extra.Propagation = &xatu.PropagationV2{
		SlotStartDiff: &wrapperspb.UInt64Value{
			Value: func() uint64 {
				diff := timestamp.Sub(slot.TimeWindow().Start()).Milliseconds()
				if diff < 0 {
					return 0
				}
				return uint64(diff)
			}(),
		},
	}

	extra.Metadata = &libp2p.TraceEventMetadata{PeerId: wrapperspb.String(e.event.PeerID)}
	extra.Topic = wrapperspb.String(e.event.Topic)
	extra.MessageId = wrapperspb.String(e.event.MessageID)
	extra.MessageSize = wrapperspb.UInt32(e.event.MessageSize)

	return extra, nil
}
