// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the quota-risk worker configuration.
//
// The worker consumes normalized runtime + exporter events from the bus,
// extracts provider rate-limit headers carried on each event, and publishes
// a `quota.risk.v1` event plus exposes Prometheus gauges.
//
// This worker is OSS-safe: it MODELS the risk; it does not enforce routing,
// throttling, or any policy decision. Enforcement is outside this worker.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration loaded from YAML.
type Config struct {
	Server   ServerConfig  `yaml:"server"`
	Bus      BusConfig     `yaml:"bus"`
	Risk     RiskConfig    `yaml:"risk"`
	Defaults DefaultLabels `yaml:"defaults"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz).
type ServerConfig struct {
	// Port is the TCP port the metrics + health server binds to. Default 8084.
	Port int `yaml:"port"`
}

// BusConfig configures the streaming bus consumer + producer.
type BusConfig struct {
	Brokers  []string `yaml:"brokers"`
	ClientID string   `yaml:"client_id"`
	// ConsumerGroup is the Kafka consumer group ID. Default
	// "openllm-quota-risk".
	ConsumerGroup string `yaml:"consumer_group"`
	// InputTopics override the default subscription set. When empty the
	// worker subscribes to both llm.runtime.normalized and
	// llm.usage.normalized so signals from gateway/SDK and exporter both
	// feed the rolling state.
	InputTopics []string `yaml:"input_topics"`
	// OutputTopic is the topic name for emitted quota.risk events.
	// Default "llm.quota.risk.v1".
	OutputTopic string `yaml:"output_topic"`
}

// RiskConfig tunes the rolling window and refresh cadence.
type RiskConfig struct {
	// WindowSeconds is how long a header observation is retained as
	// authoritative before being considered stale. Default 300 (5 min).
	WindowSeconds int `yaml:"window_seconds"`
	// RefreshIntervalSeconds is how often the worker re-emits gauges and
	// publishes a `quota.risk.v1` snapshot per key. Default 30s.
	RefreshIntervalSeconds int `yaml:"refresh_interval_seconds"`
}

// DefaultLabels are the fallback labels applied when an event arrives with
// a missing tenant. The worker still emits — it does not silently drop —
// because losing risk visibility is worse than a slightly imprecise label.
type DefaultLabels struct {
	Tenant string `yaml:"tenant"`
}

// ErrInvalidConfig is returned when validation fails.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads YAML from disk, applies defaults, and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes into a Config and validates.
func Parse(raw []byte) (*Config, error) {
	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse yaml: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8084
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-quota-risk"
	}
	if c.Bus.ConsumerGroup == "" {
		c.Bus.ConsumerGroup = "openllm-quota-risk"
	}
	if c.Bus.OutputTopic == "" {
		c.Bus.OutputTopic = "llm.quota.risk.v1"
	}
	if c.Risk.WindowSeconds == 0 {
		c.Risk.WindowSeconds = 300
	}
	if c.Risk.RefreshIntervalSeconds == 0 {
		c.Risk.RefreshIntervalSeconds = 30
	}
}

// Validate enforces invariants the worker cannot run safely without.
func (c *Config) Validate() error {
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	if c.Risk.WindowSeconds <= 0 {
		return fmt.Errorf("%w: risk.window_seconds must be > 0", ErrInvalidConfig)
	}
	if c.Risk.RefreshIntervalSeconds <= 0 {
		return fmt.Errorf("%w: risk.refresh_interval_seconds must be > 0", ErrInvalidConfig)
	}
	return nil
}

// Window returns the rolling-window retention as a Duration.
func (c *Config) Window() time.Duration {
	return time.Duration(c.Risk.WindowSeconds) * time.Second
}

// RefreshInterval returns the snapshot/refresh cadence as a Duration.
func (c *Config) RefreshInterval() time.Duration {
	return time.Duration(c.Risk.RefreshIntervalSeconds) * time.Second
}
