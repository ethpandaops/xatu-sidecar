// Package gossipsub provides Ethereum beacon chain event processing for gossipsub messages.
package gossipsub

import (
	"context"
	"fmt"
	"time"

	"github.com/ethpandaops/ethwallclock"
	v1 "github.com/ethpandaops/xatu/pkg/proto/eth/v1"
	"github.com/ethpandaops/xatu/pkg/proto/libp2p"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/google/uuid"
	ttlcache "github.com/jellydator/ttlcache/v3"
	hashstructure "github.com/mitchellh/hashstructure/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// AggregateAndProof represents a processed aggregate and proof event from gossipsub.
type AggregateAndProof struct {
	log logrus.FieldLogger

	now time.Time

	event          *RawAggregateAndProof
	clockDrift     time.Duration
	wallclock      *ethwallclock.EthereumBeaconChain
	duplicateCache *ttlcache.Cache[string, time.Time]
	clientMeta     *xatu.ClientMeta
	id             uuid.UUID
}

// RawAggregateAndProof represents the raw aggregate and proof data received from gossipsub.
type RawAggregateAndProof struct {
	PeerID              string `json:"peer_id"`
	MessageID           string `json:"message_id"`
	Slot                uint64 `json:"slot"`
	Epoch               uint64 `json:"epoch"`
	AttestationDataRoot string `json:"attestation_data_root"`
	AggregatorIndex     uint64 `json:"aggregator_index"`
	Timestamp           int64  `json:"timestamp"`
	Topic               string `json:"topic"`
	MessageSize         uint32 `json:"message_size"`
	// Additional attestation data fields
	SourceEpoch    uint64 `json:"source_epoch"`
	SourceRoot     string `json:"source_root"`
	TargetEpoch    uint64 `json:"target_epoch"`
	TargetRoot     string `json:"target_root"`
	CommitteeIndex uint64 `json:"committee_index"`
	// Aggregation and signature fields
	AggregationBits string `json:"aggregation_bits"`
	Signature       string `json:"signature"`
}

// NewAggregateAndProof creates a new AggregateAndProof instance from raw event data.
func NewAggregateAndProof(log logrus.FieldLogger, event *RawAggregateAndProof, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *AggregateAndProof {
	return &AggregateAndProof{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_AGGREGATE_AND_PROOF"),
		now:            time.UnixMilli(event.Timestamp),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the aggregate and proof event with additional metadata and returns a decorated event.
func (e *AggregateAndProof) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.Timestamp).Add(e.clockDrift)

	// Create the aggregate and proof message
	aggregateAndProof := &v1.SignedAggregateAttestationAndProofV2{
		Message: &v1.AggregateAttestationAndProofV2{
			AggregatorIndex: wrapperspb.UInt64(e.event.AggregatorIndex),
			Aggregate: &v1.AttestationV2{
				AggregationBits: e.event.AggregationBits,
				Data: &v1.AttestationDataV2{
					Slot:            wrapperspb.UInt64(e.event.Slot),
					BeaconBlockRoot: e.event.AttestationDataRoot,
					Source: &v1.CheckpointV2{
						Epoch: wrapperspb.UInt64(e.event.SourceEpoch),
						Root:  e.event.SourceRoot,
					},
					Target: &v1.CheckpointV2{
						Epoch: wrapperspb.UInt64(e.event.TargetEpoch),
						Root:  e.event.TargetRoot,
					},
					Index: wrapperspb.UInt64(e.event.CommitteeIndex),
				},
				Signature: e.event.Signature,
			},
		},
	}

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_AGGREGATE_AND_PROOF,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubAggregateAndProof{
			Libp2PTraceGossipsubAggregateAndProof: aggregateAndProof,
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.Timestamp))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra aggregate and proof data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubAggregateAndProof{
			Libp2PTraceGossipsubAggregateAndProof: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the aggregate and proof event should be ignored based on deduplication and age.
func (e *AggregateAndProof) ShouldIgnore(_ context.Context) (bool, error) {
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
			"aggregator_index":      e.event.AggregatorIndex,
		}).Debug("Duplicate aggregate and proof event received")

		return true, nil
	}

	currentSlot, _, err := e.wallclock.Now()
	if err != nil {
		return true, err
	}

	// ignore aggregate and proofs that are more than 16 slots old
	slotLimit := currentSlot.Number() - 16

	if e.event.Slot < slotLimit {
		return true, nil
	}

	return false, nil
}

func (e *AggregateAndProof) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubAggregateAndProofData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	slot := e.wallclock.Slots().FromNumber(e.event.Slot)
	epoch := e.wallclock.Epochs().FromSlot(e.event.Slot)

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubAggregateAndProofData{
		WallclockSlot: &xatu.SlotV2{
			Number:        wrapperspb.UInt64(wallclockSlot.Number()),
			StartDateTime: timestamppb.New(wallclockSlot.TimeWindow().Start()),
		},
		WallclockEpoch: &xatu.EpochV2{
			Number:        wrapperspb.UInt64(wallclockEpoch.Number()),
			StartDateTime: timestamppb.New(wallclockEpoch.TimeWindow().Start()),
		},
		Slot: &xatu.SlotV2{
			Number:        wrapperspb.UInt64(slot.Number()),
			StartDateTime: timestamppb.New(slot.TimeWindow().Start()),
		},
		Epoch: &xatu.EpochV2{
			Number:        wrapperspb.UInt64(epoch.Number()),
			StartDateTime: timestamppb.New(epoch.TimeWindow().Start()),
		},
		Propagation: &xatu.PropagationV2{
			SlotStartDiff: wrapperspb.UInt64(func() uint64 {
				diff := timestamp.Sub(slot.TimeWindow().Start()).Milliseconds()
				if diff < 0 {
					return 0
				}
				return uint64(diff)
			}()),
		},
		AggregatorIndex: wrapperspb.UInt64(e.event.AggregatorIndex),
	}

	extra.Metadata = &libp2p.TraceEventMetadata{PeerId: wrapperspb.String(e.event.PeerID)}
	extra.MessageId = wrapperspb.String(e.event.MessageID)
	extra.Topic = wrapperspb.String(e.event.Topic)
	extra.MessageSize = wrapperspb.UInt32(e.event.MessageSize)

	return extra, nil
}
