// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package consumer wires the bus client to the in-memory aggregator with
// idempotent semantics.
//
// Idempotency strategy (F010 §11):
//
//   - Every normalized event carries an event_id (UUIDv7) in the
//     `x-event-id` header AND in the JSON payload. The consumer dedups on
//     the header so the JSON body never has to be parsed twice.
//   - The dedup set is a bounded LRU, sized for the supported
//     single-replica per-tenant deployment.
//
// Replay safety (F010 §10):
//
//   - On cold start the consumer replays from the earliest retained offset
//     so the aggregator can rebuild its in-memory state. The rewind happens
//     via bus offset / group config — this package only assumes the supplied
//     EventStream delivers the events in some order; correctness does not
//     depend on order.
package consumer

import (
	"context"
	"errors"
	"sync"

	"github.com/yasvanth511/openllm-metrics-oss/apps/api/metrics-endpoint/internal/aggregator"
)

// Event is the normalized form passed from the bus adapter into the
// consumer. Headers are flattened to a string map so adapter swaps (Kafka
// vs in-memory test bus) don't leak into this package.
type Event struct {
	Topic   string
	EventID string
	Payload []byte
}

// EventStream is the narrow interface the consumer depends on. Production
// wires it to a Kafka / Redpanda consumer; tests fake it with an in-memory
// channel. The contract is: Next blocks until an event is available or ctx
// cancels, and Close releases the underlying client.
type EventStream interface {
	Next(ctx context.Context) (Event, error)
	Close()
}

// ErrStreamClosed is returned by EventStream.Next when the stream has been
// closed cleanly. Consumer.Run treats this as a normal termination.
var ErrStreamClosed = errors.New("consumer: event stream closed")

// Dedup is the narrow interface the consumer depends on for idempotency.
// Seen records the key and returns true the FIRST time the key was
// observed. Implementation here is an in-memory bounded LRU.
type Dedup interface {
	Seen(key string) bool
}

// Consumer drains an EventStream into an Aggregator, applying idempotency
// via the supplied Dedup. Safe to Run once per Consumer.
type Consumer struct {
	stream EventStream
	agg    *aggregator.Aggregator
	dedup  Dedup

	mu       sync.Mutex
	ready    bool
	consumed int64
	deduped  int64
}

// New constructs a Consumer. Pass an in-memory LRU as the Dedup for
// single-replica deployments; replace with a shared dedup store when running
// multiple replicas.
func New(stream EventStream, agg *aggregator.Aggregator, dedup Dedup) *Consumer {
	return &Consumer{
		stream: stream,
		agg:    agg,
		dedup:  dedup,
	}
}

// Run drains the stream into the aggregator until ctx cancels or the stream
// closes. Returns nil on a clean termination (context cancellation or
// ErrStreamClosed); any other error from Next is surfaced.
func (c *Consumer) Run(ctx context.Context) error {
	for {
		ev, err := c.stream.Next(ctx)
		if err != nil {
			if errors.Is(err, ErrStreamClosed) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return err
		}

		c.markReady()

		// Dedup before any heavy work so a replay storm is cheap.
		key := ev.Topic + "|" + ev.EventID
		if ev.EventID != "" && !c.dedup.Seen(key) {
			c.bumpDeduped()
			continue
		}

		_ = c.agg.Apply(ev.Topic, ev.Payload)
		c.bumpConsumed()
	}
}

// Ready returns true once the consumer has observed at least one event from
// the stream. Used by the readiness probe so /readyz signals "the
// aggregator is warm" rather than just "the process is running".
func (c *Consumer) Ready() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ready
}

// ConsumedEvents returns the total number of events forwarded to the
// aggregator (after dedup). Exposed for tests and operator observability.
func (c *Consumer) ConsumedEvents() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.consumed
}

// DedupedEvents returns the number of events the consumer dropped as
// duplicates. Exposed for tests.
func (c *Consumer) DedupedEvents() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.deduped
}

func (c *Consumer) markReady() {
	c.mu.Lock()
	c.ready = true
	c.mu.Unlock()
}

func (c *Consumer) bumpConsumed() {
	c.mu.Lock()
	c.consumed++
	c.mu.Unlock()
}

func (c *Consumer) bumpDeduped() {
	c.mu.Lock()
	c.deduped++
	c.mu.Unlock()
}
