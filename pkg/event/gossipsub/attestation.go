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

// Attestation represents a processed attestation event from gossipsub.
type Attestation struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawAttestation
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
}

// RawAttestation represents the raw attestation data received from gossipsub.
type RawAttestation struct {
	Slot                uint64 `json:"slot"`
	Epoch               uint64 `json:"epoch"`
	SubnetID            uint64 `json:"subnet_id"`
	TimestampMs         int64  `json:"timestamp_ms"`
	SourceEpoch         uint64 `json:"source_epoch"`
	TargetEpoch         uint64 `json:"target_epoch"`
	CommitteeIndex      uint64 `json:"committee_index"`
	AttesterIndex       uint64 `json:"attester_index"`
	MessageSize         uint32 `json:"message_size"`
	ShouldProcess       bool   `json:"should_process"`
	PeerID              string `json:"peer_id"`
	MessageID           string `json:"message_id"`
	AttestationDataRoot string `json:"attestation_data_root"`
	Topic               string `json:"topic"`
	SourceRoot          string `json:"source_root"`
	TargetRoot          string `json:"target_root"`
	AggregationBits     string `json:"aggregation_bits"`
	Signature           string `json:"signature"`
}

// NewAttestation creates a new Attestation instance from raw event data.
func NewAttestation(log logrus.FieldLogger, event *RawAttestation, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *Attestation {
	return &Attestation{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_BEACON_ATTESTATION"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the attestation event with additional metadata and returns a decorated event.
func (e *Attestation) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)

	// Create a basic attestation structure
	attestation := &v1.Attestation{
		AggregationBits: e.event.AggregationBits,
		Data: &v1.AttestationData{
			Slot:            e.event.Slot,
			BeaconBlockRoot: e.event.AttestationDataRoot,
			Source: &v1.Checkpoint{
				Epoch: e.event.SourceEpoch,
				Root:  e.event.SourceRoot,
			},
			Target: &v1.Checkpoint{
				Epoch: e.event.TargetEpoch,
				Root:  e.event.TargetRoot,
			},
		},
	}

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_BEACON_ATTESTATION,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubBeaconAttestation{
			Libp2PTraceGossipsubBeaconAttestation: attestation,
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra attestation data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubBeaconAttestation{
			Libp2PTraceGossipsubBeaconAttestation: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the attestation event should be ignored based on deduplication and age.
func (e *Attestation) ShouldIgnore(_ context.Context) (bool, error) {
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
			"subnet_id":             e.event.SubnetID,
		}).Debug("Duplicate attestation event received")

		return true, nil
	}

	currentSlot, _, err := e.wallclock.Now()
	if err != nil {
		return true, err
	}

	// ignore attestations that are more than 16 slots old
	slotLimit := currentSlot.Number() - 16

	if e.event.Slot < slotLimit {
		return true, nil
	}

	return false, nil
}

func (e *Attestation) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconAttestationData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	slot := e.wallclock.Slots().FromNumber(e.event.Slot)
	epoch := e.wallclock.Epochs().FromSlot(e.event.Slot)

	targetEpoch := e.wallclock.Epochs().FromSlot(e.event.TargetEpoch)
	sourceEpoch := e.wallclock.Epochs().FromSlot(e.event.SourceEpoch)

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconAttestationData{
		WallclockSlot: &xatu.SlotV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockSlot.Number()},
			StartDateTime: timestamppb.New(wallclockSlot.TimeWindow().Start()),
		},
		WallclockEpoch: &xatu.EpochV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockEpoch.Number()},
			StartDateTime: timestamppb.New(wallclockEpoch.TimeWindow().Start()),
		},
		Slot: &xatu.SlotV2{
			Number:        &wrapperspb.UInt64Value{Value: slot.Number()},
			StartDateTime: timestamppb.New(slot.TimeWindow().Start()),
		},
		Epoch: &xatu.EpochV2{
			Number:        &wrapperspb.UInt64Value{Value: epoch.Number()},
			StartDateTime: timestamppb.New(epoch.TimeWindow().Start()),
		},
		Target: &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconAttestationTargetData{
			Epoch: &xatu.EpochV2{
				StartDateTime: timestamppb.New(targetEpoch.TimeWindow().Start()),
			},
		},
		Source: &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBeaconAttestationSourceData{
			Epoch: &xatu.EpochV2{
				StartDateTime: timestamppb.New(sourceEpoch.TimeWindow().Start()),
			},
		},
		Propagation: &xatu.PropagationV2{
			SlotStartDiff: &wrapperspb.UInt64Value{
				Value: func() uint64 {
					diff := timestamp.Sub(slot.TimeWindow().Start()).Milliseconds()
					if diff < 0 {
						return 0
					}
					return uint64(diff)
				}(),
			},
		},
	}

	extra.Metadata = &libp2p.TraceEventMetadata{PeerId: wrapperspb.String(e.event.PeerID)}
	extra.MessageId = wrapperspb.String(e.event.MessageID)
	extra.Topic = wrapperspb.String(e.event.Topic)
	extra.MessageSize = wrapperspb.UInt32(e.event.MessageSize)

	extra.AttestingValidator = &xatu.AttestingValidatorV2{
		// In Electra, SingleAttestation.committee_index is the validator's position within the committee
		// In pre-Electra, it's the committee ID (same as attestation data.index)
		CommitteeIndex: &wrapperspb.UInt64Value{Value: e.event.CommitteeIndex},
		Index:          &wrapperspb.UInt64Value{Value: e.event.AttesterIndex},
	}

	return extra, nil
}
