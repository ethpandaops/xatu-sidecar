package gossipsub

import (
	"context"
	"testing"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	ttlcache "github.com/jellydator/ttlcache/v3"
	"github.com/sirupsen/logrus"
)

const (
	testSlot          = uint64(100)
	testSlotsPerEpoch = uint64(32)
	testSlotDuration  = 12 * time.Second
)

var testGenesis = time.Unix(1_700_000_000, 0) //nolint:gochecknoglobals // test fixture.

func testSlotStart() time.Time {
	return testGenesis.Add(time.Duration(testSlot) * testSlotDuration) //nolint:gosec // small constant.
}

// asInt32 mirrors xatu's ClickHouse route for Int32 propagation_slot_start_diff columns, which
// reinterprets the two's-complement uint64 proto value as int32.
func asInt32(v uint64) int32 {
	return int32(v) //nolint:gosec // intentional two's-complement reinterpretation.
}

func TestSlotStartDiffMs(t *testing.T) {
	t.Parallel()

	start := testSlotStart()

	tests := []struct {
		name     string
		offset   time.Duration
		unsigned uint64
		signed   int32
	}{
		{name: "at slot start", offset: 0, unsigned: 0, signed: 0},
		{name: "after slot start", offset: 1500 * time.Millisecond, unsigned: 1500, signed: 1500},
		{name: "before slot start", offset: -400 * time.Millisecond, unsigned: 0, signed: -400},
		{name: "one epoch before slot start", offset: -384 * time.Second, unsigned: 0, signed: -384_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			eventTime := start.Add(tt.offset)

			if got := slotStartDiffMs(eventTime, start); got != tt.unsigned {
				t.Errorf("slotStartDiffMs() = %d, want %d", got, tt.unsigned)
			}

			if got := asInt32(signedSlotStartDiffMs(eventTime, start)); got != tt.signed {
				t.Errorf("signedSlotStartDiffMs() as int32 = %d, want %d", got, tt.signed)
			}
		})
	}
}

type propagationDecorator interface {
	Decorate(ctx context.Context) (*xatu.DecoratedEvent, error)
}

type propagationGetter interface {
	GetPropagation() *xatu.PropagationV2
}

// TestDecoratePropagationPreSlot checks that events gossiped before their slot starts keep a
// negative propagation diff when their ClickHouse column is signed (bid, proposer preferences),
// and are clamped to 0 otherwise.
func TestDecoratePropagationPreSlot(t *testing.T) {
	t.Parallel()

	const offsetMs = int64(-400)

	eventMs := testSlotStart().UnixMilli() + offsetMs
	log := logrus.New()

	tests := []struct {
		name   string
		want   int32
		event  func(*ethwallclock.EthereumBeaconChain, *ttlcache.Cache[string, time.Time]) propagationDecorator
		getter func(*xatu.DecoratedEvent) propagationGetter
	}{
		{
			name: "execution payload bid is signed",
			want: int32(offsetMs),
			event: func(wc *ethwallclock.EthereumBeaconChain, cache *ttlcache.Cache[string, time.Time]) propagationDecorator {
				return NewExecutionPayloadBid(log, &RawExecutionPayloadBid{TimestampMs: eventMs, Slot: testSlot}, 0, wc, cache, &xatu.ClientMeta{})
			},
			getter: func(ev *xatu.DecoratedEvent) propagationGetter {
				return ev.GetMeta().GetClient().GetLibp2PTraceGossipsubExecutionPayloadBid()
			},
		},
		{
			name: "proposer preferences is signed",
			want: int32(offsetMs),
			event: func(wc *ethwallclock.EthereumBeaconChain, cache *ttlcache.Cache[string, time.Time]) propagationDecorator {
				return NewProposerPreferences(log, &RawProposerPreferences{TimestampMs: eventMs, Slot: testSlot}, 0, wc, cache, &xatu.ClientMeta{})
			},
			getter: func(ev *xatu.DecoratedEvent) propagationGetter {
				return ev.GetMeta().GetClient().GetLibp2PTraceGossipsubProposerPreferences()
			},
		},
		{
			name: "beacon block is clamped",
			want: 0,
			event: func(wc *ethwallclock.EthereumBeaconChain, cache *ttlcache.Cache[string, time.Time]) propagationDecorator {
				return NewBeaconBlock(log, &RawBeaconBlock{TimestampMs: eventMs, Slot: testSlot}, 0, wc, cache, &xatu.ClientMeta{})
			},
			getter: func(ev *xatu.DecoratedEvent) propagationGetter {
				return ev.GetMeta().GetClient().GetLibp2PTraceGossipsubBeaconBlock()
			},
		},
		{
			name: "execution payload envelope is clamped",
			want: 0,
			event: func(wc *ethwallclock.EthereumBeaconChain, cache *ttlcache.Cache[string, time.Time]) propagationDecorator {
				return NewExecutionPayloadEnvelope(log, &RawExecutionPayloadEnvelope{TimestampMs: eventMs, Slot: testSlot}, 0, wc, cache, &xatu.ClientMeta{})
			},
			getter: func(ev *xatu.DecoratedEvent) propagationGetter {
				return ev.GetMeta().GetClient().GetLibp2PTraceGossipsubExecutionPayloadEnvelope()
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wallclock := ethwallclock.NewEthereumBeaconChain(testGenesis, testSlotDuration, testSlotsPerEpoch)
			cache := ttlcache.New[string, time.Time]()

			ev, err := tt.event(wallclock, cache).Decorate(context.Background())
			if err != nil {
				t.Fatalf("Decorate() error = %v", err)
			}

			data := tt.getter(ev)
			if data == nil || data.GetPropagation().GetSlotStartDiff() == nil {
				t.Fatalf("missing propagation data")
			}

			if got := asInt32(data.GetPropagation().GetSlotStartDiff().GetValue()); got != tt.want {
				t.Errorf("propagation slot start diff = %d, want %d", got, tt.want)
			}
		})
	}
}
