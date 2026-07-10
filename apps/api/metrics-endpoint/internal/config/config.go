// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the metrics-endpoint service
// configuration.
//
// The schema is intentionally small: bus connection, HTTP server bind, and a
// replay window. Every value can be overridden in YAML; the service does not
// touch any secrets, so there is no env-var indirection for credentials at
// this phase.
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level config object the metrics-endpoint binary loads.
type Config struct {
	Server ServerConfig `yaml:"server"`
	Bus    BusConfig    `yaml:"bus"`
	Replay ReplayConfig `yaml:"replay"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz, /readyz).
type ServerConfig struct {
	// Port is the TCP port the server binds to. Default 9090 (Prometheus
	// scrape convention).
	Port int `yaml:"port"`
}

// BusConfig configures the streaming-bus consumer.
type BusConfig struct {
	// Brokers is the list of Kafka / Redpanda broker addresses.
	Brokers []string `yaml:"brokers"`
	// ClientID is the Kafka client identifier.
	ClientID string `yaml:"client_id"`
	// Group is the Kafka consumer group name.
	Group string `yaml:"group"`
	// Topics is the list of topics the aggregator subscribes to. Defaults to
	// the F008 canonical normalized topics.
	Topics []string `yaml:"topics"`
}

// ReplayConfig documents the cold-start replay window. F010 §10: replay
// window aligns with bus retention (default 7 days). The value is currently
// informational only — the consumer always replays from the earliest
// retained offset, so retention itself bounds the rewind.
type ReplayConfig struct {
	// WindowHours is the expected replay depth on cold start. 0 means "use
	// bus retention". F008 default retention is 168h (7 days).
	WindowHours int `yaml:"window_hours"`
}

// ErrInvalidConfig is returned by Load / Validate when the loaded config
// violates an invariant.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads a YAML file from disk, applies defaults, and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes into a Config, applies defaults, and validates.
// Exposed separately so tests can drive it without touching disk.
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
		c.Server.Port = 9090
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-metrics-endpoint"
	}
	if c.Bus.Group == "" {
		c.Bus.Group = "openllm-metrics-endpoint"
	}
	if len(c.Bus.Topics) == 0 {
		c.Bus.Topics = []string{
			"llm.usage.normalized",
			"llm.runtime.normalized",
		}
	}
	if c.Replay.WindowHours == 0 {
		c.Replay.WindowHours = 168 // 7 days, matches platform/bus/topics.yaml
	}
}

// Validate enforces invariants the service cannot run safely without.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("%w: server.port must be 1..65535, got %d", ErrInvalidConfig, c.Server.Port)
	}
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must contain at least one address", ErrInvalidConfig)
	}
	if len(c.Bus.Topics) == 0 {
		return fmt.Errorf("%w: bus.topics must not be empty", ErrInvalidConfig)
	}
	if c.Replay.WindowHours < 0 {
		return fmt.Errorf("%w: replay.window_hours must be >= 0", ErrInvalidConfig)
	}
	return nil
}
