// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package busclient

import (
	"context"
	"fmt"

	"github.com/twmb/franz-go/pkg/kgo"
)

// Producer wraps a franz-go client for producing events to the bus.
type Producer struct {
	client *kgo.Client
}

// NewProducer creates a new Producer from the given Config.
func NewProducer(cfg Config) (*Producer, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.ClientID(cfg.ClientID),
	)
	if err != nil {
		return nil, fmt.Errorf("busclient: NewProducer: %w", err)
	}
	return &Producer{client: client}, nil
}

// ProduceEvent sends a single event to the given topic.
// The event MUST carry a non-empty EventID and TenantID — these are validated
// before sending. Trace context from ctx is injected into message headers.
func (p *Producer) ProduceEvent(ctx context.Context, topic, eventID, tenantID string, payload []byte) error {
	if eventID == "" {
		return ErrMissingEventID
	}
	if tenantID == "" {
		return ErrMissingTenantID
	}

	headers := []kgo.RecordHeader{
		{Key: HeaderEventID, Value: []byte(eventID)},
		{Key: HeaderTenantID, Value: []byte(tenantID)},
	}
	headers = injectTraceContext(ctx, headers)

	record := &kgo.Record{
		Topic:   topic,
		Value:   payload,
		Headers: headers,
	}

	if err := p.client.ProduceSync(ctx, record).FirstErr(); err != nil {
		return fmt.Errorf("busclient: ProduceEvent to %s: %w", topic, err)
	}
	return nil
}

// Close flushes and closes the underlying Kafka client.
func (p *Producer) Close() {
	p.client.Close()
}
