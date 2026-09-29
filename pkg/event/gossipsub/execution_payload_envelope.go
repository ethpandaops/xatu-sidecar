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

// ExecutionPayloadEnvelope represents a processed ePBS execution_payload envelope event from gossipsub.
type ExecutionPayloadEnvelope struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawExecutionPayloadEnvelope
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
}

// RawExecutionPayloadEnvelope represents the raw execution payload envelope data received from gossipsub.
type RawExecutionPayloadEnvelope struct {
	TimestampMs     int64  `json:"timestamp_ms"`
	Slot            uint64 `json:"slot"`
	Epoch           uint64 `json:"epoch"`
	BuilderIndex    uint64 `json:"builder_index"`
	MessageSize     uint32 `json:"message_size"`
	PeerID          string `json:"peer_id"`
	MessageID       string `json:"message_id"`
	Topic           string `json:"topic"`
	BeaconBlockRoot string `json:"beacon_block_root"`
	BlockHash       string `json:"block_hash"`
	StateRoot       string `json:"state_root"`
}

// NewExecutionPayloadEnvelope creates a new ExecutionPayloadEnvelope instance from raw event data.
func NewExecutionPayloadEnvelope(log logrus.FieldLogger, event *RawExecutionPayloadEnvelope, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *ExecutionPayloadEnvelope {
	return &ExecutionPayloadEnvelope{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_EXECUTION_PAYLOAD_ENVELOPE"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the execution payload envelope event with additional metadata and returns a decorated event.
func (e *ExecutionPayloadEnvelope) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_EXECUTION_PAYLOAD_ENVELOPE,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubExecutionPayloadEnvelope{
			Libp2PTraceGossipsubExecutionPayloadEnvelope: &gossipsub.ExecutionPayloadEnvelope{
				Slot:            &wrapperspb.UInt64Value{Value: e.event.Slot},
				BuilderIndex:    &wrapperspb.UInt64Value{Value: e.event.BuilderIndex},
				BeaconBlockRoot: wrapperspb.String(e.event.BeaconBlockRoot),
				BlockHash:       wrapperspb.String(e.event.BlockHash),
				StateRoot:       wrapperspb.String(e.event.StateRoot),
			},
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra execution payload envelope data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubExecutionPayloadEnvelope{
			Libp2PTraceGossipsubExecutionPayloadEnvelope: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the execution payload envelope event should be ignored based on deduplication and age.
func (e *ExecutionPayloadEnvelope) ShouldIgnore(_ context.Context) (bool, error) {
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
			"builder_index":            e.event.BuilderIndex,
		}).Debug("Duplicate execution payload envelope event received")

		return true, nil
	}

	currentSlot, _, err := e.wallclock.Now()
	if err != nil {
		return true, err
	}

	// ignore envelopes that are more than 16 slots old
	// Guard against unsigned underflow when chain is young (slot < 16)
	if currentSlot.Number() >= 16 {
		slotLimit := currentSlot.Number() - 16
		if e.event.Slot < slotLimit {
			return true, nil
		}
	}

	return false, nil
}

func (e *ExecutionPayloadEnvelope) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubExecutionPayloadEnvelopeData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubExecutionPayloadEnvelopeData{
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
