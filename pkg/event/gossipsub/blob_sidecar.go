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

// BlobSidecar represents a processed blob sidecar event from gossipsub.
type BlobSidecar struct {
	log logrus.FieldLogger

	now time.Time

	event          *RawBlobSidecar
	clockDrift     time.Duration
	wallclock      *ethwallclock.EthereumBeaconChain
	duplicateCache *ttlcache.Cache[string, time.Time]
	clientMeta     *xatu.ClientMeta
	id             uuid.UUID
}

// RawBlobSidecar represents the raw blob sidecar data received from gossipsub.
type RawBlobSidecar struct {
	PeerID        string  `json:"peer_id"`
	MessageID     string  `json:"message_id"`
	Topic         string  `json:"topic"`
	MessageSize   uint32  `json:"message_size"`
	TimestampMs   int64   `json:"timestamp_ms"`
	Slot          uint64  `json:"slot"`
	Epoch         uint64  `json:"epoch"`
	BlockRoot     string  `json:"block_root"`
	ParentRoot    string  `json:"parent_root"`
	StateRoot     string  `json:"state_root"`
	ProposerIndex uint64  `json:"proposer_index"`
	BlobIndex     uint64  `json:"blob_index"`
	Client        *string `json:"client,omitempty"`
}

// NewBlobSidecar creates a new BlobSidecar instance from raw event data.
func NewBlobSidecar(log logrus.FieldLogger, event *RawBlobSidecar, clockDrift time.Duration, wallclock *ethwallclock.EthereumBeaconChain, duplicateCache *ttlcache.Cache[string, time.Time], clientMeta *xatu.ClientMeta) *BlobSidecar {
	return &BlobSidecar{
		log:            log.WithField("event", "LIBP2P_TRACE_GOSSIPSUB_BLOB_SIDECAR"),
		now:            time.UnixMilli(event.TimestampMs),
		event:          event,
		clockDrift:     clockDrift,
		wallclock:      wallclock,
		duplicateCache: duplicateCache,
		clientMeta:     clientMeta,
		id:             uuid.New(),
	}
}

// Decorate enriches the blob sidecar event with additional metadata and returns a decorated event.
func (e *BlobSidecar) Decorate(ctx context.Context) (*xatu.DecoratedEvent, error) {
	timestamp := time.UnixMilli(e.event.TimestampMs).Add(e.clockDrift)

	decoratedEvent := &xatu.DecoratedEvent{
		Event: &xatu.Event{
			Name:     xatu.Event_LIBP2P_TRACE_GOSSIPSUB_BLOB_SIDECAR,
			DateTime: timestamppb.New(timestamp),
			Id:       e.id.String(),
		},
		Meta: &xatu.Meta{
			Client: e.clientMeta,
		},
		Data: &xatu.DecoratedEvent_Libp2PTraceGossipsubBlobSidecar{
			Libp2PTraceGossipsubBlobSidecar: &gossipsub.BlobSidecar{
				Slot:          &wrapperspb.UInt64Value{Value: e.event.Slot},
				Index:         &wrapperspb.UInt64Value{Value: e.event.BlobIndex},
				ProposerIndex: &wrapperspb.UInt64Value{Value: e.event.ProposerIndex},
				ParentRoot:    wrapperspb.String(e.event.ParentRoot),
				StateRoot:     wrapperspb.String(e.event.StateRoot),
			},
		},
	}

	additionalData, err := e.getAdditionalData(ctx, time.UnixMilli(e.event.TimestampMs))
	if err != nil {
		e.log.WithError(err).Error("Failed to get extra blob sidecar data")
	} else {
		decoratedEvent.Meta.Client.AdditionalData = &xatu.ClientMeta_Libp2PTraceGossipsubBlobSidecar{
			Libp2PTraceGossipsubBlobSidecar: additionalData,
		}
	}

	return decoratedEvent, nil
}

// ShouldIgnore determines if the blob sidecar event should be ignored based on deduplication and age.
func (e *BlobSidecar) ShouldIgnore(_ context.Context) (bool, error) {
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
			"blob_index":            e.event.BlobIndex,
		}).Debug("Duplicate blob sidecar event received")

		return true, nil
	}

	currentSlot, _, err := e.wallclock.Now()
	if err != nil {
		return true, err
	}

	// ignore blobs that are more than 16 slots old
	slotLimit := currentSlot.Number() - 16

	if e.event.Slot < slotLimit {
		return true, nil
	}

	return false, nil
}

func (e *BlobSidecar) getAdditionalData(_ context.Context, timestamp time.Time) (*xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBlobSidecarData, error) {
	wallclockSlot, wallclockEpoch, err := e.wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	extra := &xatu.ClientMeta_AdditionalLibP2PTraceGossipSubBlobSidecarData{
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
