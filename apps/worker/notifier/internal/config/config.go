// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the notifier worker configuration.
//
// The notifier subscribes to alert.event.v1 events on the bus, matches each
// alert against per-tenant routing rules in Postgres, fans out to the
// configured generic-webhook and SMTP sinks, and records every delivery
// attempt in control_plane.notification_deliveries.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// AlertTopic is the canonical bus topic the notifier subscribes to.
const AlertTopic = "alert.event.v1"

// AuditTopic is the bus topic the notifier publishes config-mutation audit
// events to. F031 (append-only audit ledger) consumes from this topic.
const AuditTopic = "audit.event.v1"

// Config is the top-level YAML configuration.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
	Retry    RetryConfig    `yaml:"retry"`
}

// ServerConfig configures the HTTP surface that serves both the config CRUD
// API and the Prometheus /metrics + /healthz endpoints.
type ServerConfig struct {
	// Port the HTTP server binds to. Default 8085.
	Port int `yaml:"port"`
}

// DatabaseConfig holds the control-plane Postgres connection.
type DatabaseConfig struct {
	// DSNEnv is the environment variable name holding the Postgres DSN.
	DSNEnv string `yaml:"dsn_env"`
}

// BusConfig configures the streaming bus consumer + audit producer.
type BusConfig struct {
	Brokers    []string `yaml:"brokers"`
	ClientID   string   `yaml:"client_id"`
	GroupID    string   `yaml:"group_id"`
	AlertTopic string   `yaml:"alert_topic"`
	AuditTopic string   `yaml:"audit_topic"`
}

// RetryConfig bounds the delivery-retry loop. Exponential backoff:
//
//	delay_n = min(InitialBackoff * 2^n, MaxBackoff)
type RetryConfig struct {
	MaxAttempts        int `yaml:"max_attempts"`
	InitialBackoffMS   int `yaml:"initial_backoff_ms"`
	MaxBackoffMS       int `yaml:"max_backoff_ms"`
	PerAttemptTimeoutS int `yaml:"per_attempt_timeout_seconds"`
}

// ErrInvalidConfig is returned when validation fails.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads a YAML file from disk, applies defaults, and validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes and validates.
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
		c.Server.Port = 8085
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_CONTROL_PLANE_DSN"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-notifier"
	}
	if c.Bus.GroupID == "" {
		c.Bus.GroupID = "openllm-notifier"
	}
	if c.Bus.AlertTopic == "" {
		c.Bus.AlertTopic = AlertTopic
	}
	if c.Bus.AuditTopic == "" {
		c.Bus.AuditTopic = AuditTopic
	}
	if c.Retry.MaxAttempts == 0 {
		c.Retry.MaxAttempts = 5
	}
	if c.Retry.InitialBackoffMS == 0 {
		c.Retry.InitialBackoffMS = 500
	}
	if c.Retry.MaxBackoffMS == 0 {
		c.Retry.MaxBackoffMS = 30000
	}
	if c.Retry.PerAttemptTimeoutS == 0 {
		c.Retry.PerAttemptTimeoutS = 10
	}
}

// Validate enforces invariants the worker cannot run safely without.
func (c *Config) Validate() error {
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	if c.Retry.MaxAttempts < 1 {
		return fmt.Errorf("%w: retry.max_attempts must be >= 1", ErrInvalidConfig)
	}
	if c.Retry.InitialBackoffMS < 1 || c.Retry.MaxBackoffMS < c.Retry.InitialBackoffMS {
		return fmt.Errorf("%w: retry backoff bounds invalid", ErrInvalidConfig)
	}
	return nil
}

// InitialBackoff returns the initial retry delay as a Duration.
func (c *Config) InitialBackoff() time.Duration {
	return time.Duration(c.Retry.InitialBackoffMS) * time.Millisecond
}

// MaxBackoff returns the upper bound on retry delay as a Duration.
func (c *Config) MaxBackoff() time.Duration {
	return time.Duration(c.Retry.MaxBackoffMS) * time.Millisecond
}

// PerAttemptTimeout bounds a single delivery attempt.
func (c *Config) PerAttemptTimeout() time.Duration {
	return time.Duration(c.Retry.PerAttemptTimeoutS) * time.Second
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
func (c *Config) DSN() (string, error) {
	v := os.Getenv(c.Database.DSNEnv)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", c.Database.DSNEnv)
	}
	return v, nil
}
