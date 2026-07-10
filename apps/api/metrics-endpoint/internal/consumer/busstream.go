// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package consumer

import (
	"context"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
)

// BusStream is an EventStream backed by the franz-go consumer used by the
// rest of the platform (packages/bus-client/go). It owns the queue of
// already-fetched records and serves them to the consumer one at a time.
//
// The metrics-endpoint service is read-only against the bus — it never
// produces — so we deliberately do NOT use the busclient.Consumer (which
// owns a DLQ producer). Instead we drive a small kgo.Client directly so the
// memory and goroutine footprint stays minimal.
type BusStream struct {
	client *kgo.Client
	mu     sync.Mutex
	queue  []*kgo.Record
}

// BusStreamConfig configures the underlying Kafka client.
type BusStreamConfig struct {
	Brokers  []string
	Topics   []string
	Group    string
	ClientID string
}

// NewBusStream constructs a BusStream. Cold-start replay semantics are
// owned by the consumer-group reset policy: pass group="" to consume from
// the beginning of every partition on every restart (matches F010 §10
// "rebuild from bus replay"), or a stable group name to resume from the
// last committed offset.
func NewBusStream(cfg BusStreamConfig) (*BusStream, error) {
	opts := []kgo.Opt{
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumeTopics(cfg.Topics...),
		kgo.DisableAutoCommit(),
		// Start at the earliest offset for replay safety: without a group
		// this always rebuilds from the earliest retained record; with a
		// brand-new group it seeds from the start, while established groups
		// resume from their last committed offset. The aggregator's
		// in-memory dedup is what protects against double-counting on
		// replay.
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	}
	if cfg.Group != "" {
		opts = append(opts, kgo.ConsumerGroup(cfg.Group))
	}

	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("consumer: new bus stream: %w", err)
	}
	return &BusStream{client: client}, nil
}

// Next blocks until the next record is available, ctx cancels, or the
// underlying client is closed.
func (s *BusStream) Next(ctx context.Context) (Event, error) {
	for {
		// Drain queue first.
		s.mu.Lock()
		if len(s.queue) > 0 {
			rec := s.queue[0]
			s.queue = s.queue[1:]
			s.mu.Unlock()
			return recordToEvent(rec), nil
		}
		s.mu.Unlock()

		fetches := s.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return Event{}, ErrStreamClosed
		}
		if err := ctx.Err(); err != nil {
			return Event{}, err
		}
		var fetchErr error
		fetches.EachError(func(_ string, _ int32, err error) {
			if fetchErr == nil {
				fetchErr = err
			}
		})
		if fetchErr != nil {
			return Event{}, fmt.Errorf("consumer: fetch: %w", fetchErr)
		}

		newRecords := make([]*kgo.Record, 0)
		fetches.EachRecord(func(rec *kgo.Record) {
			newRecords = append(newRecords, rec)
		})
		if len(newRecords) == 0 {
			continue
		}
		s.mu.Lock()
		s.queue = append(s.queue, newRecords...)
		s.mu.Unlock()

		// Commit offsets best-effort; correctness comes from the
		// aggregator's idempotent dedup, not from offset durability.
		if err := s.client.CommitUncommittedOffsets(ctx); err != nil {
			// Non-fatal: a missed commit just means more replay on
			// restart — the dedup absorbs the duplicates.
			_ = err
		}
	}
}

// Close releases the underlying Kafka client.
func (s *BusStream) Close() { s.client.Close() }

func recordToEvent(rec *kgo.Record) Event {
	eventID := ""
	for _, h := range rec.Headers {
		if h.Key == busclient.HeaderEventID {
			eventID = string(h.Value)
			break
		}
	}
	return Event{
		Topic:   rec.Topic,
		EventID: eventID,
		Payload: rec.Value,
	}
}
