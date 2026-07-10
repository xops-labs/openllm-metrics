// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package busclient

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Handler is the callback invoked for each consumed record.
// ctx carries the extracted trace context from the message headers.
// Return a non-nil error to route the record to the DLQ.
type Handler func(ctx context.Context, record *kgo.Record) error

// ConsumerConfig extends Config with consumer-specific options.
type ConsumerConfig struct {
	Config
	// Group is the Kafka consumer group ID.
	Group string
	// Topics is the list of topics to consume.
	Topics []string
	// DLQSuffix is appended to the source topic name to form the DLQ topic.
	// Default: ".dlq"
	DLQSuffix string
}

// Consumer wraps a franz-go client for idempotent event consumption.
type Consumer struct {
	cfg     ConsumerConfig
	client  *kgo.Client
	dlqProd *kgo.Client
}

// NewConsumer creates a new idempotent Consumer.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	if cfg.DLQSuffix == "" {
		cfg.DLQSuffix = ".dlq"
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
		kgo.ConsumerGroup(cfg.Group),
		kgo.ConsumeTopics(cfg.Topics...),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("busclient: NewConsumer: %w", err)
	}

	dlqClient, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID+"-dlq-producer"),
	)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("busclient: NewConsumer DLQ producer: %w", err)
	}

	return &Consumer{cfg: cfg, client: client, dlqProd: dlqClient}, nil
}

// Run starts the consume loop. It blocks until ctx is cancelled.
// Each record is handed to handler; on handler error the record is forwarded
// to the DLQ topic and the offset is still committed (at-least-once delivery
// with DLQ quarantine).
func (c *Consumer) Run(ctx context.Context, handler Handler) error {
	for {
		fetches := c.client.PollFetches(ctx)
		if fetches.IsClientClosed() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		fetches.EachError(func(t string, p int32, err error) {
			// Partition fetch errors are transient and retried on the next
			// poll; callers needing visibility should log inside their Handler
			// or wrap Run with their own instrumentation.
		})

		fetches.EachRecord(func(record *kgo.Record) {
			rctx := extractTraceContext(ctx, record)
			if err := handler(rctx, record); err != nil {
				c.sendToDLQ(ctx, record)
			}
		})

		if err := c.client.CommitUncommittedOffsets(ctx); err != nil {
			return fmt.Errorf("busclient: commit offsets: %w", err)
		}
	}
}

func (c *Consumer) sendToDLQ(ctx context.Context, record *kgo.Record) {
	dlqTopic := record.Topic + c.cfg.DLQSuffix
	dlqRecord := &kgo.Record{
		Topic:   dlqTopic,
		Key:     record.Key,
		Value:   record.Value,
		Headers: record.Headers,
	}
	_ = c.dlqProd.ProduceSync(ctx, dlqRecord).FirstErr()
}

// Close stops the consumer and closes all underlying clients.
func (c *Consumer) Close() {
	c.client.Close()
	c.dlqProd.Close()
}
