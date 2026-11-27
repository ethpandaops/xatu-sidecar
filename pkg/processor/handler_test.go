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
	genesis := time.Now().Truncate(time.Second).Add(-1000 * slotDuration)

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

func TestHandleRawEventInclusionList(t *testing.T) {
	h, sink := newTestHandler(t)

	drift := 40 * time.Millisecond
	h.clockDrift.Store(int64(drift))

	currentSlot, _, err := h.wallclock.Now()
	if err != nil {
		t.Fatalf("wallclock now: %v", err)
	}

	listSlot := h.wallclock.Slots().FromNumber(currentSlot.Number() - 1)
	received := listSlot.TimeWindow().Start().Add(13500 * time.Millisecond)

	raw, err := json.Marshal(map[string]any{
		"timestamp_ms":    received.UnixMilli(),
		"slot":            listSlot.Number(),
		"validator_index": 4242,
		"dependent_root":  "0xd0d1d2d3d4d5d6d7d8d9dadbdcdddedfe0e1e2e3e4e5e6e7e8e9eaebecedeeef",
		"transactions":    []string{"0x616263", "0x"},
		"message_size":    321,
		"peer_id":         "16Uiu2HAmInclusionListPeer",
		"message_id":      "0x00aa",
		"topic":           "/eth2/aabbccdd/inclusion_list/ssz_snappy",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := h.HandleRawEvent(context.Background(), raw, EventTypeInclusionList); err != nil {
		t.Fatalf("handle raw event: %v", err)
	}

	if len(sink.events) != 1 {
		t.Fatalf("got %d events, want 1", len(sink.events))
	}

	event := sink.events[0]

	if got := event.GetEvent().GetName(); got != xatu.Event_LIBP2P_TRACE_GOSSIPSUB_INCLUSION_LIST {
		t.Fatalf("event name = %s", got)
	}

	if got, want := event.GetEvent().GetDateTime().AsTime(), received.Add(drift); !got.Equal(want) {
		t.Fatalf("event date_time = %s, want %s", got, want)
	}

	data := event.GetLibp2PTraceGossipsubInclusionList()
	if data == nil {
		t.Fatalf("missing inclusion list data")
	}

	if got := data.GetSlot().GetValue(); got != listSlot.Number() {
		t.Fatalf("slot = %d, want %d", got, listSlot.Number())
	}

	if got := data.GetValidatorIndex().GetValue(); got != 4242 {
		t.Fatalf("validator_index = %d, want 4242", got)
	}

	if got := data.GetDependentRoot().GetValue(); got != "0xd0d1d2d3d4d5d6d7d8d9dadbdcdddedfe0e1e2e3e4e5e6e7e8e9eaebecedeeef" {
		t.Fatalf("dependent_root = %s", got)
	}

	if got := data.GetTransactionsCount().GetValue(); got != 2 {
		t.Fatalf("transactions_count = %d, want 2", got)
	}

	if got := data.GetTransactionsTotalBytes().GetValue(); got != 3 {
		t.Fatalf("transactions_total_bytes = %d, want 3", got)
	}

	wantHashes := []string{
		"0x4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45",
		"0xc5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470",
	}
	if got := data.GetTransactionHashes(); len(got) != len(wantHashes) || got[0] != wantHashes[0] || got[1] != wantHashes[1] {
		t.Fatalf("transaction_hashes = %v, want %v", got, wantHashes)
	}

	additional := event.GetMeta().GetClient().GetLibp2PTraceGossipsubInclusionList()
	if additional == nil {
		t.Fatalf("missing inclusion list additional data")
	}

	if got := additional.GetSlot().GetNumber().GetValue(); got != listSlot.Number() {
		t.Fatalf("additional slot = %d, want %d", got, listSlot.Number())
	}

	if got := additional.GetWallclockSlot().GetNumber().GetValue(); got != currentSlot.Number() {
		t.Fatalf("wallclock slot = %d, want %d", got, currentSlot.Number())
	}

	if got, want := additional.GetEpoch().GetNumber().GetValue(), listSlot.Number()/32; got != want {
		t.Fatalf("epoch = %d, want %d", got, want)
	}

	if got := additional.GetPropagation().GetSlotStartDiff().GetValue(); got != 13540 {
		t.Fatalf("slot_start_diff = %d, want 13540", got)
	}

	if got := additional.GetMetadata().GetPeerId().GetValue(); got != "16Uiu2HAmInclusionListPeer" {
		t.Fatalf("peer_id = %s", got)
	}

	if got := additional.GetTopic().GetValue(); got != "/eth2/aabbccdd/inclusion_list/ssz_snappy" {
		t.Fatalf("topic = %s", got)
	}

	if got := additional.GetMessageId().GetValue(); got != "0x00aa" {
		t.Fatalf("message_id = %s", got)
	}

	if got := additional.GetMessageSize().GetValue(); got != 321 {
		t.Fatalf("message_size = %d, want 321", got)
	}
}

func TestHandleRawEventInclusionListRejectsInvalidTransaction(t *testing.T) {
	h, sink := newTestHandler(t)

	raw, err := json.Marshal(map[string]any{
		"timestamp_ms": time.Now().UnixMilli(),
		"slot":         1,
		"transactions": []string{"0x01", "0xzz"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if err := h.HandleRawEvent(context.Background(), raw, EventTypeInclusionList); err == nil {
		t.Fatalf("expected an error for an undecodable transaction")
	}

	if len(sink.events) != 0 {
		t.Fatalf("got %d events, want 0", len(sink.events))
	}
}
