package gossipsub

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu-sidecar/pkg/cache"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/sirupsen/logrus"
)

const (
	testCurrentSlot = 1000
	testBlockRoot   = "0x3a5ef0b0dc0a3a7f6a8a5f3e7f5f1c5bdb0d3c7a5d9e5f0a1b2c3d4e5f60718"
	testPeerID      = "16Uiu2HAmPartialPeer"
	testTopic       = "/eth2/aabbccdd/data_column_sidecar_7/ssz_snappy"
)

type partialFixture struct {
	wallclock  *ethwallclock.EthereumBeaconChain
	duplicates *cache.DuplicateCache
	blockSlots *cache.BlockSlotCache
	log        logrus.FieldLogger
	now        time.Time
}

func newPartialFixture(t *testing.T) *partialFixture {
	t.Helper()

	slotDuration := 12 * time.Second
	now := time.Now()
	genesis := now.Add(-testCurrentSlot * slotDuration).Add(-time.Second)

	wallclock := ethwallclock.NewEthereumBeaconChain(genesis, slotDuration, 32)
	t.Cleanup(wallclock.Stop)

	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)

	return &partialFixture{
		wallclock:  wallclock,
		duplicates: cache.NewDuplicateCache(),
		blockSlots: cache.NewBlockSlotCache(),
		log:        log,
		now:        now,
	}
}

func (f *partialFixture) partial(raw *RawPartialDataColumn) *PartialDataColumn {
	return NewPartialDataColumn(f.log, raw, 0, f.wallclock, f.duplicates.GossipsubPartialDataColumn, f.blockSlots, &xatu.ClientMeta{})
}

func (f *partialFixture) completed(raw *RawDataColumnCompleted) *DataColumnCompleted {
	return NewDataColumnCompleted(f.log, raw, 0, f.wallclock, f.duplicates.DataColumnCompleted, f.blockSlots, &xatu.ClientMeta{})
}

func (f *partialFixture) outcome(raw *RawPartialDataColumnOutcome) *PartialDataColumnOutcome {
	return NewPartialDataColumnOutcome(f.log, raw, 0, f.wallclock, f.duplicates.GossipsubPartialDataColumnOutcome, f.blockSlots, &xatu.ClientMeta{})
}

func recvPart(timestamp time.Time, slot uint64) *RawPartialDataColumn {
	return &RawPartialDataColumn{
		TimestampMs:            timestamp.UnixMilli(),
		Direction:              PartialDirectionRecv,
		CapturePoint:           PartialCapturePointApp,
		PeerID:                 testPeerID,
		Topic:                  testTopic,
		GroupID:                "0x00" + testBlockRoot[2:],
		GroupVersion:           0,
		BlockRoot:              testBlockRoot,
		Slot:                   slot,
		ColumnIndex:            7,
		HasMessage:             true,
		MessageSize:            4242,
		CellsCount:             2,
		CellsBitmap:            "0x0d",
		BitmapLength:           3,
		HasHeader:              slot != 0,
		KZGCommitmentsCount:    3,
		ProposerIndex:          99,
		HasMetadata:            true,
		MetadataSize:           10,
		MetadataAvailableCount: 2,
		MetadataRequestsCount:  3,
		MetadataAvailable:      "0x0d",
		MetadataRequests:       "0x0f",
	}
}

func TestPartialDataColumnDecorate(t *testing.T) {
	f := newPartialFixture(t)
	raw := recvPart(f.now, testCurrentSlot-1)

	event, err := f.partial(raw).Decorate(context.Background())
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	if got := event.GetEvent().GetName(); got != xatu.Event_LIBP2P_TRACE_GOSSIPSUB_PARTIAL_DATA_COLUMN {
		t.Fatalf("event name = %v", got)
	}

	data := event.GetLibp2PTraceGossipsubPartialDataColumn()
	checks := map[string][2]any{
		"direction":         {data.GetDirection().GetValue(), PartialDirectionRecv},
		"capture_point":     {data.GetCapturePoint().GetValue(), PartialCapturePointApp},
		"group_id":          {data.GetGroupId().GetValue(), raw.GroupID},
		"block_root":        {data.GetBlockRoot().GetValue(), testBlockRoot},
		"column_index":      {data.GetColumnIndex().GetValue(), uint64(7)},
		"cells_count":       {data.GetCellsCount().GetValue(), uint32(2)},
		"cells_bitmap":      {data.GetCellsBitmap().GetValue(), "0x0d"},
		"bitmap_length":     {data.GetBitmapLength().GetValue(), uint32(3)},
		"proposer_index":    {data.GetProposerIndex().GetValue(), uint64(99)},
		"metadata_requests": {data.GetMetadataRequests().GetValue(), "0x0f"},
	}

	for field, pair := range checks {
		if pair[0] != pair[1] {
			t.Errorf("%s = %v, want %v", field, pair[0], pair[1])
		}
	}

	extra := event.GetMeta().GetClient().GetLibp2PTraceGossipsubPartialDataColumn()
	if got := extra.GetSlot().GetNumber().GetValue(); got != testCurrentSlot-1 {
		t.Errorf("slot = %d, want %d", got, testCurrentSlot-1)
	}

	if got := extra.GetEpoch().GetNumber().GetValue(); got != (testCurrentSlot-1)/32 {
		t.Errorf("epoch = %d", got)
	}

	if got := extra.GetPropagation().GetSlotStartDiff().GetValue(); got < 12000 || got > 14000 {
		t.Errorf("slot start diff = %d, want about one slot", got)
	}

	if got := extra.GetMetadata().GetPeerId().GetValue(); got != testPeerID {
		t.Errorf("peer = %q", got)
	}

	if got := extra.GetMessageSize().GetValue(); got != 4242 {
		t.Errorf("message size = %d", got)
	}

	if got := extra.GetTopic().GetValue(); got != testTopic {
		t.Errorf("topic = %q", got)
	}
}

func TestPartialDataColumnMetadataOnlySend(t *testing.T) {
	f := newPartialFixture(t)
	raw := &RawPartialDataColumn{
		TimestampMs:  f.now.UnixMilli(),
		Direction:    PartialDirectionSend,
		CapturePoint: PartialCapturePointApp,
		Topic:        testTopic,
		GroupID:      "0x00" + testBlockRoot[2:],
		BlockRoot:    testBlockRoot,
		ColumnIndex:  7,
	}

	event, err := f.partial(raw).Decorate(context.Background())
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	data := event.GetLibp2PTraceGossipsubPartialDataColumn()
	if data.GetCellsBitmap() != nil || data.GetMetadataAvailable() != nil {
		t.Errorf("empty bitmaps should be unset, got %v / %v", data.GetCellsBitmap(), data.GetMetadataAvailable())
	}

	extra := event.GetMeta().GetClient().GetLibp2PTraceGossipsubPartialDataColumn()
	if extra.GetMetadata() != nil {
		t.Errorf("metadata should be unset without a peer, got %v", extra.GetMetadata())
	}

	if extra.GetSlot() != nil || extra.GetPropagation() != nil {
		t.Errorf("slot context should be unset for an unknown slot")
	}

	if extra.GetWallclockSlot().GetNumber().GetValue() != testCurrentSlot {
		t.Errorf("wallclock slot = %d", extra.GetWallclockSlot().GetNumber().GetValue())
	}
}

func TestPartialSlotResolvedFromEarlierEvents(t *testing.T) {
	f := newPartialFixture(t)

	headerless := f.partial(recvPart(f.now, 0))
	if headerless.slot != 0 {
		t.Fatalf("slot = %d before any header was seen", headerless.slot)
	}

	f.partial(recvPart(f.now, testCurrentSlot-2))

	later := f.partial(recvPart(f.now.Add(time.Millisecond), 0))
	if later.slot != testCurrentSlot-2 {
		t.Fatalf("slot = %d, want it learned from the header part", later.slot)
	}

	outcome := f.outcome(&RawPartialDataColumnOutcome{BlockRoot: testBlockRoot})
	if outcome.slot != testCurrentSlot-2 {
		t.Fatalf("outcome slot = %d", outcome.slot)
	}
}

func TestPartialDataColumnShouldIgnore(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(raw *RawPartialDataColumn)
		ignore  bool
		wantErr error
	}{
		{name: "recent", mutate: func(*RawPartialDataColumn) {}},
		{name: "unknown slot is kept", mutate: func(raw *RawPartialDataColumn) { raw.Slot = 0; raw.BlockRoot = "" }},
		{name: "stale", mutate: func(raw *RawPartialDataColumn) { raw.Slot = testCurrentSlot - 20 }, ignore: true},
		{name: "bad direction", mutate: func(raw *RawPartialDataColumn) { raw.Direction = "recv" }, ignore: true, wantErr: ErrInvalidPartialDirection},
		{name: "bad capture point", mutate: func(raw *RawPartialDataColumn) { raw.CapturePoint = "" }, ignore: true, wantErr: ErrInvalidPartialCapturePoint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newPartialFixture(t)
			raw := recvPart(f.now, testCurrentSlot)
			tt.mutate(raw)

			ignore, err := f.partial(raw).ShouldIgnore(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}

			if ignore != tt.ignore {
				t.Fatalf("ignore = %v, want %v", ignore, tt.ignore)
			}
		})
	}
}

func TestPartialDataColumnDropsExactDuplicates(t *testing.T) {
	f := newPartialFixture(t)

	first, err := f.partial(recvPart(f.now, testCurrentSlot)).ShouldIgnore(context.Background())
	if err != nil || first {
		t.Fatalf("first: ignore=%v err=%v", first, err)
	}

	again, err := f.partial(recvPart(f.now, testCurrentSlot)).ShouldIgnore(context.Background())
	if err != nil || !again {
		t.Fatalf("duplicate: ignore=%v err=%v", again, err)
	}

	later, err := f.partial(recvPart(f.now.Add(time.Millisecond), testCurrentSlot)).ShouldIgnore(context.Background())
	if err != nil || later {
		t.Fatalf("later part: ignore=%v err=%v", later, err)
	}
}

func TestDataColumnCompletedDedupesPerColumn(t *testing.T) {
	f := newPartialFixture(t)
	completion := func(via string, column uint64, offset time.Duration) *RawDataColumnCompleted {
		return &RawDataColumnCompleted{
			TimestampMs:  f.now.Add(offset).UnixMilli(),
			BlockRoot:    testBlockRoot,
			Slot:         testCurrentSlot,
			ColumnIndex:  column,
			CompletedVia: via,
			CellsTotal:   6,
		}
	}

	tests := []struct {
		raw    *RawDataColumnCompleted
		ignore bool
	}{
		{raw: completion(DataColumnCompletedViaPartialGossip, 3, 0)},
		{raw: completion(DataColumnCompletedViaEL, 3, time.Second), ignore: true},
		{raw: completion(DataColumnCompletedViaEL, 4, time.Second)},
	}

	for i, tt := range tests {
		ignore, err := f.completed(tt.raw).ShouldIgnore(context.Background())
		if err != nil {
			t.Fatalf("completion %d: %v", i, err)
		}

		if ignore != tt.ignore {
			t.Fatalf("completion %d: ignore = %v, want %v", i, ignore, tt.ignore)
		}
	}

	if _, err := f.completed(completion("GOSSIP", 5, 0)).ShouldIgnore(context.Background()); !errors.Is(err, ErrInvalidCompletedVia) {
		t.Fatalf("err = %v, want ErrInvalidCompletedVia", err)
	}
}

func TestDataColumnCompletedDecorate(t *testing.T) {
	f := newPartialFixture(t)
	raw := &RawDataColumnCompleted{
		TimestampMs:         f.now.UnixMilli(),
		BlockRoot:           testBlockRoot,
		Slot:                testCurrentSlot,
		ColumnIndex:         3,
		CompletedVia:        DataColumnCompletedViaEL,
		CellsTotal:          6,
		CellsFromGossip:     2,
		CellsFromEL:         4,
		ContributingPeers:   1,
		KZGCommitmentsCount: 6,
	}

	event, err := f.completed(raw).Decorate(context.Background())
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	data := event.GetLibp2PTraceDataColumnCompleted()
	if data.GetCompletedVia().GetValue() != DataColumnCompletedViaEL || data.GetCellsFromEl().GetValue() != 4 || data.GetCellsFromGossip().GetValue() != 2 {
		t.Errorf("unexpected completion data: %v", data)
	}

	extra := event.GetMeta().GetClient().GetLibp2PTraceDataColumnCompleted()
	if extra.GetMetadata() != nil {
		t.Errorf("EL completion should carry no peer")
	}

	if extra.GetSlot().GetNumber().GetValue() != testCurrentSlot {
		t.Errorf("slot = %d", extra.GetSlot().GetNumber().GetValue())
	}

	if f.blockSlots.Resolve(testBlockRoot, 0) != testCurrentSlot {
		t.Errorf("completion should record its block slot")
	}
}

func TestPartialDataColumnOutcome(t *testing.T) {
	f := newPartialFixture(t)
	raw := &RawPartialDataColumnOutcome{
		TimestampMs:   f.now.UnixMilli(),
		PeerID:        testPeerID,
		Topic:         testTopic,
		GroupID:       "0x00" + testBlockRoot[2:],
		BlockRoot:     testBlockRoot,
		Slot:          testCurrentSlot,
		ColumnIndex:   7,
		Stage:         PartialOutcomeStageCells,
		Outcome:       PartialOutcomeAccept,
		CellsCount:    3,
		NewCellsCount: 1,
	}

	ignore, err := f.outcome(raw).ShouldIgnore(context.Background())
	if err != nil || ignore {
		t.Fatalf("ignore=%v err=%v", ignore, err)
	}

	event, err := f.outcome(raw).Decorate(context.Background())
	if err != nil {
		t.Fatalf("decorate: %v", err)
	}

	data := event.GetLibp2PTraceGossipsubPartialDataColumnOutcome()
	if data.GetOutcome().GetValue() != PartialOutcomeAccept || data.GetNewCellsCount().GetValue() != 1 || data.GetReason() != nil {
		t.Errorf("unexpected outcome data: %v", data)
	}

	if got := event.GetMeta().GetClient().GetLibp2PTraceGossipsubPartialDataColumnOutcome().GetMetadata().GetPeerId().GetValue(); got != testPeerID {
		t.Errorf("peer = %q", got)
	}

	bad := *raw
	bad.Stage = "VERIFY"

	if _, err := f.outcome(&bad).ShouldIgnore(context.Background()); !errors.Is(err, ErrInvalidPartialStage) {
		t.Fatalf("err = %v, want ErrInvalidPartialStage", err)
	}

	bad = *raw
	bad.Outcome = "VALID"

	if _, err := f.outcome(&bad).ShouldIgnore(context.Background()); !errors.Is(err, ErrInvalidPartialOutcome) {
		t.Fatalf("err = %v, want ErrInvalidPartialOutcome", err)
	}
}

func TestRawPartialPayloadsDecodeFromJSON(t *testing.T) {
	payload := `{"event_type":"PARTIAL_DATA_COLUMN","timestamp_ms":1,"direction":"RECV","capture_point":"APP",` +
		`"peer_id":"p","topic":"t","group_id":"0x01","group_version":1,"block_root":"0xab","slot":5,"column_index":9,` +
		`"has_message":true,"message_size":12,"cells_count":1,"cells_bitmap":"0x03","bitmap_length":1,"has_header":false,` +
		`"kzg_commitments_count":0,"proposer_index":0,"has_metadata":true,"metadata_size":10,` +
		`"metadata_available_count":1,"metadata_requests_count":1,"metadata_available":"0x03","metadata_requests":"0x03"}`

	var raw RawPartialDataColumn
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if raw.GroupVersion != 1 || raw.ColumnIndex != 9 || raw.MetadataRequests != "0x03" || !raw.HasMetadata {
		t.Fatalf("unexpected decode: %+v", raw)
	}
}
