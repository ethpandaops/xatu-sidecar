// Package main provides the Go-to-C FFI interface for Xatu sidecar integration.
package main

/*
#include <stdlib.h>
*/
import "C"
import (
	"context"
	"encoding/json"
	"unsafe"

	"github.com/ethpandaops/xatu-sidecar/pkg/processor"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Global instances.
var (
	//nolint:gochecknoglobals // Used by FFI
	log logrus.FieldLogger
	//nolint:gochecknoglobals // Used by FFI
	handler *processor.Handler
)

type Config struct {
	LogLevel  *string           `yaml:"log_level,omitempty"`
	Processor *processor.Config `yaml:"processor"`
}

//export Init
func Init(configJSON *byte) int32 {
	l := logrus.New()
	log = l.WithField("namespace", "xatu")

	// Parse configuration
	if configJSON == nil {
		log.Error("No configuration provided")
		return -1
	}

	goConfigData := C.GoString((*C.char)(unsafe.Pointer(configJSON)))

	var config *Config

	if err := yaml.Unmarshal([]byte(goConfigData), &config); err != nil {
		log.WithError(err).Error("Failed to parse configuration")
		return -1
	}

	// Set log level if provided
	if config.LogLevel != nil {
		switch *config.LogLevel {
		case "trace":
			l.SetLevel(logrus.TraceLevel)
		case "debug":
			l.SetLevel(logrus.DebugLevel)
		case "info":
			l.SetLevel(logrus.InfoLevel)
		case "warn", "warning":
			l.SetLevel(logrus.WarnLevel)
		case "error":
			l.SetLevel(logrus.ErrorLevel)
		default:
			l.SetLevel(logrus.InfoLevel)
		}
	}

	// Set default NTP server if not specified
	if config.Processor.NTPServer == "" {
		config.Processor.NTPServer = "time.google.com"
	}

	var err error
	handler, err = processor.NewHandler(context.Background(), log, config.Processor)
	if err != nil {
		log.WithError(err).Error("Failed to create processor handler")

		return -1
	}

	if err := handler.Start(context.Background()); err != nil {
		log.WithError(err).Error("Failed to start handler")
		return -1
	}

	return 0
}

//export SendEventBatch
func SendEventBatch(events *byte) int32 {
	defer func() {
		if r := recover(); r != nil {
			log.WithField("panic", r).Error("Recovered from panic in SendEventBatch")
		}
	}()

	if handler == nil {
		log.Error("not initialized")

		return -1
	}

	if events == nil {
		log.Error("events is nil")

		return -2
	}

	// Parse JSON array of events
	goEventsData := C.GoString((*C.char)(unsafe.Pointer(events)))

	jsonLen := len(goEventsData)

	if jsonLen == 0 {
		log.Error("Empty JSON data")

		return -2
	}

	var rawEvents []json.RawMessage
	if err := json.Unmarshal([]byte(goEventsData), &rawEvents); err != nil {
		log.WithError(err).WithField("json_length", jsonLen).Error("Failed to parse events array")

		return -2
	}

	log.WithField("batch_size", len(rawEvents)).Debug("Processing event batch")

	// Process each event in the batch
	for i, rawEvent := range rawEvents {
		// First get the event type
		var eventTypeData struct {
			EventType processor.EventType `json:"event_type"`
		}

		if err := json.Unmarshal(rawEvent, &eventTypeData); err != nil {
			log.WithError(err).WithField("event_index", i).Error("Failed to parse event type")

			continue
		}

		err := handler.HandleRawEvent(context.Background(), rawEvent, eventTypeData.EventType)
		if err != nil {
			log.WithError(err).WithField("event_index", i).Error("Failed to handle event")

			continue
		}
	}

	return 0
}

//export Shutdown
func Shutdown() {
	log.Info("shutdown")

	if handler != nil {
		if err := handler.Stop(context.Background()); err != nil {
			log.WithError(err).Error("Failed to stop handler")
		}
	}
}

//export GetStatus
func GetStatus() *byte {
	status := map[string]interface{}{
		"initialized": handler != nil,
		"mode":        "in-process",
	}

	jsonBytes, err := json.Marshal(status)
	if err != nil {
		return (*byte)(unsafe.Pointer(C.CString(`{"error": "failed to marshal status"}`)))
	}

	return (*byte)(unsafe.Pointer(C.CString(string(jsonBytes))))
}

//export FreeString
func FreeString(s *byte) {
	C.free(unsafe.Pointer(s))
}

func main() {} // Required for building
