// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package llmproviderreceiver

import (
	"errors"
	"fmt"
	"time"
)

// Config is the user-facing configuration for the llmprovider receiver.
//
// It is decoded from the OTel Collector config block:
//
//	receivers:
//	  llmprovider:
//	    bus:
//	      brokers: [redpanda:9092]
//	      topic:   llm.runtime.normalized
//	      group_id: otelcol-llmprovider
//	    consumer:
//	      session_timeout: 30s
//	      poll_timeout:    5s
//
// All bus identifiers are configurable so the same binary can attach to a
// Redpanda cluster, a Kafka cluster, or any compatible broker without
// rebuilding the component.
type Config struct {
	// Bus configures the upstream broker connection. The receiver consumes
	// the runtime.event.v1 contract (TopicRuntimeNormalized) and translates
	// each event into OTLP metrics.
	Bus BusConfig `mapstructure:"bus"`

	// Consumer tunes the franz-go consumer-group behavior. Defaults are
	// applied in Validate() so most operators leave this block empty.
	Consumer ConsumerOptions `mapstructure:"consumer"`
}

// BusConfig holds broker-side identifiers.
type BusConfig struct {
	// Brokers is the seed broker list (host:port). Required.
	Brokers []string `mapstructure:"brokers"`

	// Topic is the canonical runtime.event.v1 topic. Defaults to
	// "llm.runtime.normalized" — the F008 TopicRuntimeNormalized constant.
	Topic string `mapstructure:"topic"`

	// GroupID is the Kafka consumer group. One group per receiver
	// deployment so multiple Collector replicas share partitions. Required.
	GroupID string `mapstructure:"group_id"`

	// ClientID is the franz-go client identifier reported to the broker.
	// Defaults to "otelcol-llmprovider".
	ClientID string `mapstructure:"client_id"`
}

// ConsumerOptions exposes the franz-go knobs the receiver tunes per-deploy.
type ConsumerOptions struct {
	// SessionTimeout bounds the broker-side consumer-group session.
	// Defaults to 30s.
	SessionTimeout time.Duration `mapstructure:"session_timeout"`

	// PollTimeout bounds each PollFetches call. The receiver loop wakes at
	// most this often to check for ctx cancellation. Defaults to 5s.
	PollTimeout time.Duration `mapstructure:"poll_timeout"`

	// MaxRecordsPerPoll caps the records ingested per poll cycle. Used to
	// flatten bursty backlog into evenly-sized metric batches downstream.
	// Defaults to 1024.
	MaxRecordsPerPoll int `mapstructure:"max_records_per_poll"`
}

// Errors surfaced by Validate so callers can use errors.Is in their tests.
var (
	ErrNoBrokers = errors.New("llmproviderreceiver: bus.brokers must not be empty")
	ErrNoGroup   = errors.New("llmproviderreceiver: bus.group_id is required")
)

// Validate enforces the invariants the receiver cannot run safely without.
// Defaults are applied here so the receiver constructor sees a fully-populated
// Config — keep this method free of side effects beyond filling zero values.
func (c *Config) Validate() error {
	if len(c.Bus.Brokers) == 0 {
		return ErrNoBrokers
	}
	if c.Bus.GroupID == "" {
		return ErrNoGroup
	}
	if c.Bus.Topic == "" {
		// Default to the F008 canonical runtime topic.
		c.Bus.Topic = "llm.runtime.normalized"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "otelcol-llmprovider"
	}
	if c.Consumer.SessionTimeout <= 0 {
		c.Consumer.SessionTimeout = 30 * time.Second
	}
	if c.Consumer.PollTimeout <= 0 {
		c.Consumer.PollTimeout = 5 * time.Second
	}
	if c.Consumer.MaxRecordsPerPoll <= 0 {
		c.Consumer.MaxRecordsPerPoll = 1024
	}
	// Defensive guard against a wildly small poll timeout that would peg the CPU.
	if c.Consumer.PollTimeout < 100*time.Millisecond {
		return fmt.Errorf("llmproviderreceiver: consumer.poll_timeout must be >= 100ms, got %s", c.Consumer.PollTimeout)
	}
	return nil
}
