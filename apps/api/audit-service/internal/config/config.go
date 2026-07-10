// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the audit-service configuration.
//
// The service has three external surfaces:
//
//   - Postgres (the audit ledger schema, read+append; never UPDATE/DELETE).
//   - Streaming bus (audit.event.v1 subscriber).
//   - HTTP API (read queries, export, verify).
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration object the binary loads.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
}

// ServerConfig configures the HTTP read API.
type ServerConfig struct {
	// Port is the TCP port the HTTP server binds to. Default 8090.
	Port int `yaml:"port"`
	// MaxPageSize caps the rows returned by a single /v1/audit/entries call.
	MaxPageSize int `yaml:"max_page_size"`
	// DefaultPageSize is the page size when the caller omits ?limit=.
	DefaultPageSize int `yaml:"default_page_size"`
}

// DatabaseConfig holds the audit Postgres connection.
type DatabaseConfig struct {
	// DSNEnv is the environment variable name holding the Postgres DSN.
	// Default OPENLLM_AUDIT_DSN.
	DSNEnv string `yaml:"dsn_env"`
}

// BusConfig configures the streaming-bus consumer for audit.event.v1.
type BusConfig struct {
	// Brokers is the list of Kafka / Redpanda broker addresses.
	Brokers []string `yaml:"brokers"`
	// ClientID is the Kafka client identifier.
	ClientID string `yaml:"client_id"`
	// Group is the Kafka consumer group name.
	Group string `yaml:"group"`
	// Topic is the bus topic carrying audit events. Default audit.event.v1.
	Topic string `yaml:"topic"`
}

// Topic name constant — the audit event topic exposed on the bus.
const TopicAuditEvent = "audit.event.v1"

// ErrInvalidConfig is returned when validation fails.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads a YAML config file from disk, applies defaults, and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes into a Config, applies defaults, and validates.
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
		c.Server.Port = 8090
	}
	if c.Server.MaxPageSize == 0 {
		c.Server.MaxPageSize = 500
	}
	if c.Server.DefaultPageSize == 0 {
		c.Server.DefaultPageSize = 50
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_AUDIT_DSN"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-audit-service"
	}
	if c.Bus.Group == "" {
		c.Bus.Group = "openllm-audit-service"
	}
	if c.Bus.Topic == "" {
		c.Bus.Topic = TopicAuditEvent
	}
}

// Validate enforces the invariants the service cannot run safely without.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("%w: server.port must be 1..65535, got %d", ErrInvalidConfig, c.Server.Port)
	}
	if c.Server.MaxPageSize <= 0 {
		return fmt.Errorf("%w: server.max_page_size must be > 0", ErrInvalidConfig)
	}
	if c.Server.DefaultPageSize <= 0 || c.Server.DefaultPageSize > c.Server.MaxPageSize {
		return fmt.Errorf("%w: server.default_page_size must be > 0 and <= max_page_size", ErrInvalidConfig)
	}
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	return nil
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
func (c *Config) DSN() (string, error) {
	v := os.Getenv(c.Database.DSNEnv)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", c.Database.DSNEnv)
	}
	return v, nil
}

// ShutdownTimeout returns the HTTP server shutdown grace period.
func (c *Config) ShutdownTimeout() time.Duration { return 5 * time.Second }
