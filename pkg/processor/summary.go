package processor

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// Summary is a struct that holds the summary of the processor.
type Summary struct {
	log           logrus.FieldLogger
	printInterval time.Duration

	eventStreamEvents sync.Map
	eventsExported    atomic.Uint64
	failedEvents      atomic.Uint64
}

// NewSummary creates a new summary with the given print interval.
func NewSummary(log logrus.FieldLogger, printInterval time.Duration) *Summary {
	return &Summary{
		log:           log,
		printInterval: printInterval,
	}
}

// Start begins the summary printing loop.
func (s *Summary) Start(ctx context.Context) {
	s.log.WithField("interval", s.printInterval).Info("Starting summary")
	ticker := time.NewTicker(s.printInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Print()
		}
	}
}

// Print outputs the current summary statistics.
func (s *Summary) Print() {
	events := s.GetEventStreamEvents()

	// Build a sorted slice of event stream topics and counts
	type topicCount struct {
		topic string
		count uint64
	}

	sortedEvents := make([]topicCount, 0, len(events))
	var totalEvents uint64
	for topic, count := range events {
		sortedEvents = append(sortedEvents, topicCount{topic, count})
		totalEvents += count
	}

	sort.Slice(sortedEvents, func(i, j int) bool {
		return sortedEvents[i].count > sortedEvents[j].count
	})

	// Log summary header
	s.log.WithFields(logrus.Fields{
		"total_events":    totalEvents,
		"events_exported": s.GetEventsExported(),
		"events_failed":   s.GetFailedEvents(),
		"interval":        s.printInterval,
	}).Info("Event summary")

	// Log per-event type breakdown if there are events
	if len(sortedEvents) > 0 {
		s.log.Info("Event breakdown:")
		for _, tc := range sortedEvents {
			percentage := float64(tc.count) / float64(totalEvents) * 100
			s.log.WithFields(logrus.Fields{
				"count":      tc.count,
				"percentage": fmt.Sprintf("%.1f%%", percentage),
			}).Infof("  %s", tc.topic)
		}
	}

	s.Reset()
}

// AddEventsExported increments the exported events counter.
func (s *Summary) AddEventsExported(count uint64) {
	s.eventsExported.Add(count)
}

// GetEventsExported returns the current count of exported events.
func (s *Summary) GetEventsExported() uint64 {
	return s.eventsExported.Load()
}

// AddFailedEvents increments the failed events counter.
func (s *Summary) AddFailedEvents(count uint64) {
	s.failedEvents.Add(count)
}

// GetFailedEvents returns the current count of failed events.
func (s *Summary) GetFailedEvents() uint64 {
	return s.failedEvents.Load()
}

// AddEventStreamEvents increments the counter for a specific event topic.
func (s *Summary) AddEventStreamEvents(topic string, count uint64) {
	current, _ := s.eventStreamEvents.LoadOrStore(topic, count)

	s.eventStreamEvents.Store(topic, current.(uint64)+count)
}

// GetEventStreamEvents returns a map of all event topics and their counts.
func (s *Summary) GetEventStreamEvents() map[string]uint64 {
	events := make(map[string]uint64)

	s.eventStreamEvents.Range(func(key, value any) bool {
		events[key.(string)], _ = value.(uint64)

		return true
	})

	return events
}

// Reset clears all summary counters.
func (s *Summary) Reset() {
	s.eventsExported.Store(0)
	s.failedEvents.Store(0)
	s.eventStreamEvents = sync.Map{}
}
