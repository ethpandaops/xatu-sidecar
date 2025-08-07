package processor

import (
	"context"
	"encoding/json"
	"errors"

	//nolint:gosec // only exposed if pprofAddr config is set
	_ "net/http/pprof"
	"runtime"
	"time"

	"github.com/beevik/ntp"
	"github.com/ethpandaops/ethwallclock"
	"github.com/ethpandaops/xatu-sidecar/pkg/cache"
	"github.com/ethpandaops/xatu-sidecar/pkg/event/gossipsub"
	"github.com/ethpandaops/xatu-sidecar/pkg/version"
	"github.com/ethpandaops/xatu/pkg/output"
	"github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/go-co-op/gocron/v2"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

const unknown = "unknown"

// Define static errors.
var (
	ErrConfigRequired        = errors.New("config is required")
	ErrFailedCreateWallclock = errors.New("failed to create wallclock")
	ErrUnsupportedEventType  = errors.New("unsupported event type")
)

// Handler processes blockchain events from the sidecar.
type Handler struct {
	sinks          []output.Sink
	Config         *Config
	duplicateCache *cache.DuplicateCache
	wallclock      *ethwallclock.EthereumBeaconChain
	summary        *Summary
	log            logrus.FieldLogger
	scheduler      gocron.Scheduler
	id             uuid.UUID
	clockDrift     time.Duration
}

// EventType represents the type of blockchain event.
type EventType string

const (
	// EventTypeBeaconBlock represents a beacon block event.
	EventTypeBeaconBlock EventType = "BEACON_BLOCK"
	// EventTypeAttestation represents an attestation event.
	EventTypeAttestation EventType = "ATTESTATION"
	// EventTypeAggregateAndProof represents an aggregate and proof event.
	EventTypeAggregateAndProof EventType = "AGGREGATE_AND_PROOF"
	// EventTypeBlobSidecar represents a blob sidecar event.
	EventTypeBlobSidecar EventType = "BLOB_SIDECAR"
	// EventTypeDataColumnSidecar represents a data column sidecar event.
	EventTypeDataColumnSidecar EventType = "DATA_COLUMN_SIDECAR"
)

// NewHandler creates a new Handler instance.
func NewHandler(_ context.Context, log logrus.FieldLogger, config *Config) (*Handler, error) {
	log = log.WithField("module", "processor")

	if config == nil {
		return nil, ErrConfigRequired
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	sinks, err := config.CreateSinks(log)
	if err != nil {
		return nil, err
	}

	wallclock := ethwallclock.NewEthereumBeaconChain(time.Unix(int64(config.Ethereum.GenesisTime), 0), time.Duration(config.Ethereum.SecondsPerSlot)*time.Second, config.Ethereum.SlotsPerEpoch) //nolint:gosec // genesis time won't overflow
	if wallclock == nil {
		return nil, ErrFailedCreateWallclock
	}

	duplicateCache := cache.NewDuplicateCache()
	duplicateCache.Start()

	scheduler, err := gocron.NewScheduler(gocron.WithLocation(time.Local))
	if err != nil {
		return nil, err
	}

	s := &Handler{
		Config:         config,
		sinks:          sinks,
		clockDrift:     time.Duration(0),
		log:            log,
		duplicateCache: duplicateCache,
		wallclock:      wallclock,
		id:             uuid.New(),
		summary:        NewSummary(log, time.Duration(60)*time.Second),
		scheduler:      scheduler,
	}

	return s, nil
}

// Start initializes and starts the handler.
func (h *Handler) Start(ctx context.Context) error {
	h.log.
		WithField("version", version.Full()).
		WithField("id", h.id.String()).
		Info("Starting xatu-sidecar")

	if err := h.startCrons(ctx); err != nil {
		h.log.WithError(err).Fatal("Failed to start crons")
	}

	for _, sink := range h.sinks {
		h.log.WithField("type", sink.Type()).WithField("name", sink.Name()).Info("Starting sink")

		if err := sink.Start(ctx); err != nil {
			return err
		}
	}

	go h.summary.Start(ctx)

	return nil
}

// HandleRawEvent processes a raw event based on its type.
//
//nolint:gocyclo // This function handles multiple event types in a switch statement
func (h *Handler) HandleRawEvent(ctx context.Context, rawEvent json.RawMessage, eventType EventType) error {
	clientMeta := h.createNewClientMeta(ctx)

	switch eventType {
	case EventTypeBeaconBlock:
		var eventData gossipsub.RawBeaconBlock
		if err := json.Unmarshal(rawEvent, &eventData); err != nil {
			return err
		}

		beaconBlock := gossipsub.NewBeaconBlock(h.log, &eventData, h.clockDrift, h.wallclock, h.duplicateCache.GossipsubBeaconBlock, clientMeta)

		ignore, err := beaconBlock.ShouldIgnore(ctx)
		if err != nil {
			return err
		}

		if ignore {
			return nil
		}

		decoratedEvent, err := beaconBlock.Decorate(ctx)
		if err != nil {
			return err
		}

		return h.handleNewDecoratedEvent(ctx, decoratedEvent)

	case EventTypeAttestation:
		var eventData gossipsub.RawAttestation
		if err := json.Unmarshal(rawEvent, &eventData); err != nil {
			return err
		}

		attestation := gossipsub.NewAttestation(h.log, &eventData, h.clockDrift, h.wallclock, h.duplicateCache.GossipsubAttestation, clientMeta)

		ignore, err := attestation.ShouldIgnore(ctx)
		if err != nil {
			return err
		}

		if ignore {
			return nil
		}

		decoratedEvent, err := attestation.Decorate(ctx)
		if err != nil {
			return err
		}

		return h.handleNewDecoratedEvent(ctx, decoratedEvent)

	case EventTypeAggregateAndProof:
		var eventData gossipsub.RawAggregateAndProof
		if err := json.Unmarshal(rawEvent, &eventData); err != nil {
			return err
		}

		aggregateAndProof := gossipsub.NewAggregateAndProof(h.log, &eventData, h.clockDrift, h.wallclock, h.duplicateCache.GossipsubAggregateAndProof, clientMeta)

		ignore, err := aggregateAndProof.ShouldIgnore(ctx)
		if err != nil {
			return err
		}

		if ignore {
			return nil
		}

		decoratedEvent, err := aggregateAndProof.Decorate(ctx)
		if err != nil {
			return err
		}

		return h.handleNewDecoratedEvent(ctx, decoratedEvent)

	case EventTypeBlobSidecar:
		var eventData gossipsub.RawBlobSidecar
		if err := json.Unmarshal(rawEvent, &eventData); err != nil {
			return err
		}

		blobSidecar := gossipsub.NewBlobSidecar(h.log, &eventData, h.clockDrift, h.wallclock, h.duplicateCache.GossipsubBlobSidecar, clientMeta)

		ignore, err := blobSidecar.ShouldIgnore(ctx)
		if err != nil {
			return err
		}

		if ignore {
			return nil
		}

		decoratedEvent, err := blobSidecar.Decorate(ctx)
		if err != nil {
			return err
		}

		return h.handleNewDecoratedEvent(ctx, decoratedEvent)

	case EventTypeDataColumnSidecar:
		var eventData gossipsub.RawDataColumnSidecar
		if err := json.Unmarshal(rawEvent, &eventData); err != nil {
			return err
		}

		dataColumnSidecar := gossipsub.NewDataColumnSidecar(h.log, &eventData, h.clockDrift, h.wallclock, h.duplicateCache.GossipsubDataColumnSidecar, clientMeta)

		ignore, err := dataColumnSidecar.ShouldIgnore(ctx)
		if err != nil {
			return err
		}

		if ignore {
			return nil
		}

		decoratedEvent, err := dataColumnSidecar.Decorate(ctx)
		if err != nil {
			return err
		}

		return h.handleNewDecoratedEvent(ctx, decoratedEvent)

	default:
		return ErrUnsupportedEventType
	}
}

func (h *Handler) createNewClientMeta(_ context.Context) *xatu.ClientMeta {
	return &xatu.ClientMeta{
		Name:           h.Config.Name,
		Version:        version.Short(),
		Id:             h.id.String(),
		Implementation: "Xatu Sidecar",
		ModuleName:     xatu.ModuleName_SIDECAR,
		Os:             runtime.GOOS,
		ClockDrift:     uint64(h.clockDrift.Milliseconds()), //nolint:gosec // milliseconds won't overflow
		Ethereum: &xatu.ClientMeta_Ethereum{
			Network: &xatu.ClientMeta_Ethereum_Network{
				Name: h.Config.Ethereum.Network.Name,
				Id:   h.Config.Ethereum.Network.ID,
			},
			Execution: &xatu.ClientMeta_Ethereum_Execution{},
			Consensus: &xatu.ClientMeta_Ethereum_Consensus{
				Implementation: h.Config.Client.Name,
				Version:        h.Config.Client.Version,
			},
		},
		Labels: map[string]string{},
	}
}

func (h *Handler) startCrons(ctx context.Context) error {
	if _, err := h.scheduler.NewJob(
		gocron.DurationJob(5*time.Minute),
		gocron.NewTask(
			func(ctx context.Context) {
				if err := h.syncClockDrift(ctx); err != nil {
					h.log.WithError(err).Error("Failed to sync clock drift")
				}
			},
			ctx,
		),
		gocron.WithStartAt(gocron.WithStartImmediately()),
	); err != nil {
		return err
	}

	h.scheduler.Start()

	return nil
}

func (h *Handler) syncClockDrift(_ context.Context) error {
	response, err := ntp.Query(h.Config.NTPServer)
	if err != nil {
		return err
	}

	err = response.Validate()
	if err != nil {
		return err
	}

	h.clockDrift = response.ClockOffset

	h.log.WithField("drift", h.clockDrift).Debug("Updated clock drift")

	if h.clockDrift > 2*time.Second || h.clockDrift < -2*time.Second {
		h.log.WithField("drift", h.clockDrift).Warn("Large clock drift detected, consider configuring an NTP server on your instance")
	}

	return err
}

func (h *Handler) handleNewDecoratedEvent(ctx context.Context, event *xatu.DecoratedEvent) error {
	eventType := event.GetEvent().GetName().String()
	if eventType == "" {
		eventType = unknown
	}

	h.summary.AddEventsExported(1)
	h.summary.AddEventStreamEvents(eventType, 1)

	for _, sink := range h.sinks {
		if err := sink.HandleNewDecoratedEvent(ctx, event); err != nil {
			h.log.
				WithError(err).
				WithField("sink", sink.Type()).
				WithField("event_type", event.GetEvent().GetName()).
				Error("Failed to send event to sink")
		}
	}

	return nil
}

// Stop gracefully shuts down the handler.
func (h *Handler) Stop(ctx context.Context) error {
	for _, sink := range h.sinks {
		if err := sink.Stop(ctx); err != nil {
			return err
		}
	}

	if h.scheduler != nil {
		if err := h.scheduler.Shutdown(); err != nil {
			return err
		}
	}

	if h.wallclock != nil {
		h.wallclock.Stop()
	}

	return nil
}
