package gossipsub

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu/pkg/proto/libp2p"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Partial data column directions. Values mirror xatu's clmimicry payload constants.
const (
	PartialDirectionRecv = "RECV"
	PartialDirectionSend = "SEND"
	PartialDirectionDrop = "DROP"
)

// Partial data column capture points.
const (
	PartialCapturePointWire = "WIRE"
	PartialCapturePointApp  = "APP"
)

// Data column completion paths.
const (
	DataColumnCompletedViaFullGossip     = "FULL_GOSSIP"
	DataColumnCompletedViaPartialGossip  = "PARTIAL_GOSSIP"
	DataColumnCompletedViaEL             = "EL"
	DataColumnCompletedViaReconstruction = "RECONSTRUCTION"
	DataColumnCompletedViaUnknown        = "UNKNOWN"
)

// Partial data column validation stages and outcomes.
const (
	PartialOutcomeStageDecode = "DECODE"
	PartialOutcomeStageHeader = "HEADER"
	PartialOutcomeStageCells  = "CELLS"

	PartialOutcomeAccept = "ACCEPT"
	PartialOutcomeIgnore = "IGNORE"
	PartialOutcomeReject = "REJECT"
)

// maxPartialSlotAge is how many slots behind the wallclock a partial data column event may be.
const maxPartialSlotAge = 16

// Errors returned for partial data column payloads carrying an unknown enum value.
var (
	ErrInvalidPartialDirection    = errors.New("invalid partial data column direction")
	ErrInvalidPartialCapturePoint = errors.New("invalid partial data column capture point")
	ErrInvalidCompletedVia        = errors.New("invalid data column completed_via")
	ErrInvalidPartialStage        = errors.New("invalid partial data column outcome stage")
	ErrInvalidPartialOutcome      = errors.New("invalid partial data column outcome")
)

func validateEnum(value string, allowed []string, err error) error {
	if slices.Contains(allowed, value) {
		return nil
	}

	return fmt.Errorf("%w: %q", err, value)
}

// blockTiming holds the wallclock and block slot context shared by the partial data column events.
// Slot, Epoch and Propagation are nil when the block slot is unknown.
type blockTiming struct {
	WallclockSlot  *xatu.SlotV2
	WallclockEpoch *xatu.EpochV2
	Slot           *xatu.SlotV2
	Epoch          *xatu.EpochV2
	Propagation    *xatu.PropagationV2
}

// newBlockTiming derives the timing context from the client-reported timestamp. Like the other
// gossipsub events, propagation is measured on the uncorrected client clock.
func newBlockTiming(wallclock *ethwallclock.EthereumBeaconChain, timestamp time.Time, slotNumber uint64) (*blockTiming, error) {
	wallclockSlot, wallclockEpoch, err := wallclock.FromTime(timestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to get wallclock time: %w", err)
	}

	timing := &blockTiming{
		WallclockSlot: &xatu.SlotV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockSlot.Number()},
			StartDateTime: timestamppb.New(wallclockSlot.TimeWindow().Start()),
		},
		WallclockEpoch: &xatu.EpochV2{
			Number:        &wrapperspb.UInt64Value{Value: wallclockEpoch.Number()},
			StartDateTime: timestamppb.New(wallclockEpoch.TimeWindow().Start()),
		},
	}

	if slotNumber == 0 {
		return timing, nil
	}

	slot := wallclock.Slots().FromNumber(slotNumber)
	epoch := wallclock.Epochs().FromSlot(slotNumber)

	timing.Slot = &xatu.SlotV2{
		Number:        &wrapperspb.UInt64Value{Value: slotNumber},
		StartDateTime: timestamppb.New(slot.TimeWindow().Start()),
	}
	timing.Epoch = &xatu.EpochV2{
		Number:        &wrapperspb.UInt64Value{Value: epoch.Number()},
		StartDateTime: timestamppb.New(epoch.TimeWindow().Start()),
	}

	diff := timestamp.Sub(slot.TimeWindow().Start()).Milliseconds()
	if diff < 0 {
		diff = 0
	}

	timing.Propagation = &xatu.PropagationV2{
		SlotStartDiff: &wrapperspb.UInt64Value{Value: uint64(diff)},
	}

	return timing, nil
}

// isStaleSlot reports whether a known block slot is too far behind the wallclock to export.
func isStaleSlot(wallclock *ethwallclock.EthereumBeaconChain, slot uint64) (bool, error) {
	if slot == 0 {
		return false, nil
	}

	currentSlot, _, err := wallclock.Now()
	if err != nil {
		return true, err
	}

	return currentSlot.Number() >= maxPartialSlotAge && slot < currentSlot.Number()-maxPartialSlotAge, nil
}

// peerMetadata returns trace metadata for peerID, or nil when no peer is attached to the event.
func peerMetadata(peerID string) *libp2p.TraceEventMetadata {
	if peerID == "" {
		return nil
	}

	return &libp2p.TraceEventMetadata{PeerId: wrapperspb.String(peerID)}
}

// optionalString wraps value, or returns nil for an empty value.
func optionalString(value string) *wrapperspb.StringValue {
	if value == "" {
		return nil
	}

	return wrapperspb.String(value)
}
