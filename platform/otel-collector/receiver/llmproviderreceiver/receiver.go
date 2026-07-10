// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package llmproviderreceiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

// llmReceiver is the receiver.Metrics implementation. It owns a franz-go
// consumer, decodes runtime.event.v1 records into the canonical Go shape,
// hands them to the translator, and pushes the resulting pmetric.Metrics into
// the Collector pipeline.
type llmReceiver struct {
	cfg      *Config
	settings receiver.Settings
	logger   *zap.Logger
	next     consumer.Metrics

	translator *Translator

	mu     sync.Mutex
	client *kgo.Client

	cancel context.CancelFunc
	done   chan struct{}
}

// newReceiver constructs a receiver but does not yet start the consume loop.
// Start() owns lifecycle so the Collector can build the pipeline before the
// first record lands.
func newReceiver(set receiver.Settings, cfg *Config, next consumer.Metrics) (*llmReceiver, error) {
	if next == nil {
		return nil, errors.New("llmproviderreceiver: nil next consumer")
	}
	return &llmReceiver{
		cfg:        cfg,
		settings:   set,
		logger:     set.Logger,
		next:       next,
		translator: NewTranslator(),
		done:       make(chan struct{}),
	}, nil
}

// Start opens the bus client and spawns the consume loop. The Collector
// guarantees Start is called once, before any records are expected, and that
// Shutdown is called before the process exits.
//
// Lifecycle invariants:
//   - All errors during broker dial bubble back to the Collector so it can
//     fail the pipeline cleanly.
//   - The consume loop runs on a fresh context cancelled by Shutdown — the
//     caller's Start context is for boot only, not for run.
//   - The translator is stateless from cycle to cycle so a partition rebalance
//     does not require state migration.
func (r *llmReceiver) Start(_ context.Context, _ component.Host) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.client != nil {
		return errors.New("llmproviderreceiver: already started")
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(r.cfg.Bus.Brokers...),
		kgo.ClientID(r.cfg.Bus.ClientID),
		kgo.ConsumerGroup(r.cfg.Bus.GroupID),
		kgo.ConsumeTopics(r.cfg.Bus.Topic),
		kgo.SessionTimeout(r.cfg.Consumer.SessionTimeout),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return fmt.Errorf("llmproviderreceiver: dial brokers: %w", err)
	}
	r.client = client

	// Collector lifecycle contract: Start's ctx covers startup only; the
	// consume loop must outlive it and stop via Shutdown -> r.cancel.
	runCtx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	go r.run(runCtx) //nolint:contextcheck

	r.logger.Info("llmprovider receiver started",
		zap.String("topic", r.cfg.Bus.Topic),
		zap.String("group_id", r.cfg.Bus.GroupID),
		zap.Strings("brokers", r.cfg.Bus.Brokers),
	)
	return nil
}

// run is the consume loop. It blocks on PollFetches up to PollTimeout and
// exits when ctx is cancelled — i.e. when Shutdown is called.
//
// Logging policy: log only counts and offsets, never event payloads.
// runtime.event.v1 carries tenant identifiers (a privacy concern) and request
// hashes (not prompts/completions, but still high-cardinality identifiers).
// Anything more than "received N records at offset O" is forbidden here.
func (r *llmReceiver) run(ctx context.Context) {
	defer close(r.done)

	for {
		if err := ctx.Err(); err != nil {
			return
		}

		// PollFetches respects ctx cancellation; the explicit check above is
		// for the case where the previous iteration cancelled mid-handling.
		pollCtx, cancel := context.WithTimeout(ctx, r.cfg.Consumer.PollTimeout)
		fetches := r.client.PollRecords(pollCtx, r.cfg.Consumer.MaxRecordsPerPoll)
		cancel()

		if fetches.IsClientClosed() {
			return
		}
		fetches.EachError(func(t string, p int32, err error) {
			r.logger.Warn("partition fetch error",
				zap.String("topic", t),
				zap.Int32("partition", p),
				zap.Error(err),
			)
		})

		var events []RuntimeEvent
		recordCount := 0
		fetches.EachRecord(func(rec *kgo.Record) {
			recordCount++
			var ev RuntimeEvent
			if err := json.Unmarshal(rec.Value, &ev); err != nil {
				// Bad payload — log topic/offset only (NOT the value).
				r.logger.Warn("skip undecodable record",
					zap.String("topic", rec.Topic),
					zap.Int32("partition", rec.Partition),
					zap.Int64("offset", rec.Offset),
				)
				return
			}
			events = append(events, ev)
		})

		if len(events) > 0 {
			metrics := r.translator.Translate(events)
			if metrics.DataPointCount() > 0 {
				if err := r.next.ConsumeMetrics(ctx, metrics); err != nil {
					r.logger.Error("downstream consumer rejected metrics batch",
						zap.Int("event_count", len(events)),
						zap.Error(err),
					)
					// Do not commit; the next poll will reprocess.
					continue
				}
			}
		}

		if recordCount > 0 {
			if err := r.client.CommitUncommittedOffsets(ctx); err != nil {
				if !errors.Is(err, context.Canceled) {
					r.logger.Error("commit offsets failed",
						zap.Int("record_count", recordCount),
						zap.Error(err),
					)
				}
			}
			r.logger.Debug("batch processed",
				zap.Int("record_count", recordCount),
				zap.Int("event_count", len(events)),
			)
		}
	}
}

// Shutdown stops the consume loop and closes the broker client. The Collector
// blocks here until run() returns, so a stuck broker call can stall shutdown —
// the PollTimeout bound in run() guarantees the loop wakes at least that often.
func (r *llmReceiver) Shutdown(ctx context.Context) error {
	r.mu.Lock()
	cancel := r.cancel
	client := r.client
	r.cancel = nil
	r.client = nil
	r.mu.Unlock()

	if cancel == nil {
		return nil
	}
	cancel()

	select {
	case <-r.done:
	case <-ctx.Done():
		// Collector gave up waiting; close the client anyway so file
		// descriptors are released.
	}
	if client != nil {
		client.Close()
	}
	r.logger.Info("llmprovider receiver stopped")
	return nil
}
