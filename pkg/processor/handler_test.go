package processor

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu-sidecar/pkg/cache"
	"github.com/ethpandaops/xatu/pkg/output"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/sirupsen/logrus"
)

type captureSink struct {
	events []*xatu.DecoratedEvent
}

func (s *captureSink) Start(context.Context) error { return nil }
func (s *captureSink) Stop(context.Context) error  { return nil }
func (s *captureSink) Type() string                { return "capture" }
func (s *captureSink) Name() string                { return "capture" }

func (s *captureSink) HandleNewDecoratedEvent(_ context.Context, event *xatu.DecoratedEvent) error {
	s.events = append(s.events, event)

	return nil
}

func (s *captureSink) HandleNewDecoratedEvents(_ context.Context, events []*xatu.DecoratedEvent) error {
	s.events = append(s.events, events...)

	return nil
}

func TestClockDriftMillis(t *testing.T) {
	tests := []struct {
		name  string
		drift time.Duration
		want  uint64
	}{
		{name: "zero", drift: 0, want: 0},
		{name: "positive", drift: 32 * time.Millisecond, want: 32},
		{name: "negative", drift: -32 * time.Millisecond, want: 32},
		{name: "negative sub millisecond", drift: -900 * time.Microsecond, want: 0},
		{name: "negative seconds", drift: -2500 * time.Millisecond, want: 2500},
		{name: "minimum duration", drift: time.Duration(math.MinInt64), want: 9223372036854},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clockDriftMillis(tt.drift); got != tt.want {
				t.Fatalf("clockDriftMillis(%s) = %d, want %d", tt.drift, got, tt.want)
			}
		})
	}
}

func TestHandleRawEventClockDrift(t *testing.T) {
	tests := []struct {
		name      string
		drift     time.Duration
		wantDrift uint64
	}{
		{name: "positive", drift: 32 * time.Millisecond, wantDrift: 32},
		{name: "negative", drift: -32 * time.Millisecond, wantDrift: 32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, sink := newTestHandler(t)
			h.clockDrift.Store(int64(tt.drift))

			now := time.Now().Truncate(time.Millisecond)
			currentSlot, _, err := h.wallclock.Now()
			if err != nil {
				t.Fatalf("wallclock now: %v", err)
			}

			raw, err := json.Marshal(map[string]any{
				"timestamp_ms":   now.UnixMilli(),
				"slot":           currentSlot.Number(),
				"proposer_index": 7,
				"peer_id":        "16Uiu2HAmDriftPeer",
				"message_id":     "drift",
				"topic":          "/eth2/aabbccdd/beacon_block/ssz_snappy",
				"block_root":     "0x01",
			})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			if err := h.HandleRawEvent(context.Background(), raw, EventTypeBeaconBlock); err != nil {
				t.Fatalf("handle raw event: %v", err)
			}

			if len(sink.events) != 1 {
				t.Fatalf("got %d events, want 1", len(sink.events))
			}

			event := sink.events[0]

			if got := event.GetMeta().GetClient().GetClockDrift(); got != tt.wantDrift {
				t.Fatalf("clock_drift = %d, want %d", got, tt.wantDrift)
			}

			if got, want := event.GetEvent().GetDateTime().AsTime(), now.Add(tt.drift); !got.Equal(want) {
				t.Fatalf("event date_time = %s, want %s", got, want)
			}
		})
	}
}

func newTestHandler(t *testing.T) (*Handler, *captureSink) {
	t.Helper()

	slotDuration := 12 * time.Second
	genesis := time.Now().Add(-1000 * slotDuration)

	wallclock := ethwallclock.NewEthereumBeaconChain(genesis, slotDuration, 32)
	t.Cleanup(wallclock.Stop)

	blockSlots := cache.NewBlockSlotCache()

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	sink := &captureSink{}

	return &Handler{
		Config:         &Config{Name: "test"},
		sinks:          []output.Sink{sink},
		duplicateCache: cache.NewDuplicateCache(),
		blockSlots:     blockSlots,
		wallclock:      wallclock,
		summary:        NewSummary(log, time.Minute),
		log:            log,
	}, sink
}
