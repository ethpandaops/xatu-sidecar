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

// ProposerPreferences represents a processed ePBS proposer_preferences event from gossipsub.
type ProposerPreferences struct {
	duplicateCache *ttlcache.Cache[string, time.Time]
	event          *RawProposerPreferences
	wallclock      *ethwallclock.EthereumBeaconChain
	clientMeta     *xatu.ClientMeta
	log            logrus.FieldLogger
	now            time.Time
	id             uuid.UUID
	clockDrift     time.Duration
}

// RawProposerPreferences represents the raw proposer preferences data received from gossipsub.
// Slot carries the preference's proposal slot.
type RawProposerPreferences struct {
	TimestampMs    int64  `json:"timestamp_ms"`
	Slot           uint64 `json:"slot"`
	Epoch          uint64 `json:"epoch"`
	ValidatorIndex uint64 `json:"validator_index"`
	TargetGasLimit uint64 `json:"target_gas_limit"`
	MessageSize    uint32 `json:"message_size"`
	PeerID         string `json:"peer_id"`
	MessageID      string `json:"message_id"`
	Topic          string `json:"topic"`
	FeeRecipient   string `json:"fee_recipient"`
}

// NewProposerPreferences creates a new ProposerPreferences instance from raw event data.
func NewProposerPreferences(log logrus.FieldLogger, event *RawProposerPreferences, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *ProposerPreferences {
	return &ProposerPreferences{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_PROPOSER_PREFERENCES"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the proposer preferences event with additional metadata and returns a decorated event.
func (e *ProposerPreferences) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_PROPOSER_PREFERENCES,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubProposerPreferences{
			Libp2PTraceGossipsubProposerPreferences: &gossipsub.ProposerPreferences{
				Slot:           &wrapperspb.UInt64Value{Value: e.event.Slot},
				ValidatorIndex: &wrapperspb.UInt64Value{Value: e.event.ValidatorIndex},
				FeeRecipient:   wrapperspb.String(e.event.FeeRecipient),
				TargetGasLimit: &wrapperspb.UInt64Value{Value: e.event.TargetGasLimit},
			},
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra proposer preferences data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubProposerPreferences{
			Libp2PTraceGossipsubProposerPreferences: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the proposer preferences event should be ignored based on deduplication.
// Unlike block/sidecar events there is no age cutoff: preferences are published ahead of the
// proposal slot, so recency relative to the wallclock is not a validity signal.
func (e *ProposerPreferences) ShouldIgnore(_ context.Context) (bool, error) {
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
			"validator_index":          e.event.ValidatorIndex,
		}).Debug("Duplicate proposer preferences event received")

		return true, nil
	}

	return false, nil
}

func (e *ProposerPreferences) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubProposerPreferencesData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubProposerPreferencesData{
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
