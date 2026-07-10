// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the reconciler-worker configuration.
//
// The reconciler subscribes to two bus topics:
//
//   - llm.cost.estimated   — runtime-side cost estimates from cost-mapper
//     (source = gateway | sdk).
//   - llm.usage.reconciled — vendor-reconciled cost from focus-ingester
//     (source = exporter).
//
// It publishes one new topic:
//
//   - reconciliation.window.v1 (default topic: llm.reconciliation.window) —
//     emitted when a window closes. Used by F033 alerting and F027
//     dashboards. The reconciler itself never makes routing or budget
//     decisions on the drift.
//
// Knobs:
//   - window.size_seconds              (default 3600s, 1 hour)
//   - window.grace_seconds             (default 172800s, 48 hours — providers
//     can lag substantially on billing
//     finalization, see
//     docs/architecture/reconciliation.md)
//   - closer.scan_interval_seconds     (default 300s)
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level YAML shape.
type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Window   WindowConfig   `yaml:"window"`
	Closer   CloserConfig   `yaml:"closer"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
	Defaults DefaultLabels  `yaml:"defaults"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz).
type ServerConfig struct {
	Port int `yaml:"port"`
}

// WindowConfig configures the join window and the reconciliation grace
// period. The grace period exists because provider billing lags — FOCUS
// data for hour H may not appear in the exporter until hours or days later.
type WindowConfig struct {
	// SizeSeconds is the correlation-window size (default 3600 = 1 hour).
	SizeSeconds int `yaml:"size_seconds"`
	// GraceSeconds is how long after window_end a window stays 'open'
	// waiting for late-arriving reconciled events (default 172800 = 48h).
	GraceSeconds int `yaml:"grace_seconds"`
}

// CloserConfig configures the periodic closer scan.
type CloserConfig struct {
	// ScanIntervalSeconds is how often the closer scans Postgres for
	// windows whose grace period has elapsed (default 300 = 5 min).
	ScanIntervalSeconds int `yaml:"scan_interval_seconds"`
}

// DatabaseConfig is the control-plane Postgres connection.
type DatabaseConfig struct {
	DSNEnv string `yaml:"dsn_env"`
}

// BusConfig configures the bus consumer + producer.
type BusConfig struct {
	Brokers       []string `yaml:"brokers"`
	ClientID      string   `yaml:"client_id"`
	ConsumerGroup string   `yaml:"consumer_group"`
	// Topics the reconciler consumes. The set is fixed but exposed in
	// config so operators can re-route during incident recovery.
	EstimatedTopic  string `yaml:"estimated_topic"`
	ReconciledTopic string `yaml:"reconciled_topic"`
	// Topic the reconciler publishes window-close events to.
	WindowTopic string `yaml:"window_topic"`
}

// DefaultLabels are stamped on self-observability counters; they do not
// override per-event {tenant, team, app, env, project} labels.
type DefaultLabels struct {
	Tenant string `yaml:"tenant"`
	Env    string `yaml:"env"`
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
	if c.Window.SizeSeconds == 0 {
		c.Window.SizeSeconds = 3600
	}
	if c.Window.GraceSeconds == 0 {
		c.Window.GraceSeconds = 172800
	}
	if c.Closer.ScanIntervalSeconds == 0 {
		c.Closer.ScanIntervalSeconds = 300
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_CONTROL_PLANE_DSN"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-reconciler"
	}
	if c.Bus.ConsumerGroup == "" {
		c.Bus.ConsumerGroup = "openllm-reconciler"
	}
	if c.Bus.EstimatedTopic == "" {
		c.Bus.EstimatedTopic = "llm.cost.estimated"
	}
	if c.Bus.ReconciledTopic == "" {
		c.Bus.ReconciledTopic = "llm.usage.reconciled"
	}
	if c.Bus.WindowTopic == "" {
		c.Bus.WindowTopic = "llm.reconciliation.window"
	}
}

// Validate enforces invariants the worker cannot run safely without.
func (c *Config) Validate() error {
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	if c.Defaults.Tenant == "" {
		return fmt.Errorf("%w: defaults.tenant is required (used for self-observability labels)", ErrInvalidConfig)
	}
	switch c.Defaults.Env {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("%w: defaults.env must be development|staging|production, got %q",
			ErrInvalidConfig, c.Defaults.Env)
	}
	if c.Window.SizeSeconds <= 0 {
		return fmt.Errorf("%w: window.size_seconds must be > 0", ErrInvalidConfig)
	}
	if c.Window.GraceSeconds < 0 {
		return fmt.Errorf("%w: window.grace_seconds must be >= 0", ErrInvalidConfig)
	}
	if c.Closer.ScanIntervalSeconds <= 0 {
		return fmt.Errorf("%w: closer.scan_interval_seconds must be > 0", ErrInvalidConfig)
	}
	return nil
}

// WindowSize returns the window size as a Duration.
func (c *Config) WindowSize() time.Duration {
	return time.Duration(c.Window.SizeSeconds) * time.Second
}

// GracePeriod returns the reconciliation grace period as a Duration.
func (c *Config) GracePeriod() time.Duration {
	return time.Duration(c.Window.GraceSeconds) * time.Second
}

// ScanInterval returns the closer-scan interval as a Duration.
func (c *Config) ScanInterval() time.Duration {
	return time.Duration(c.Closer.ScanIntervalSeconds) * time.Second
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
func (c *Config) DSN() (string, error) {
	v := os.Getenv(c.Database.DSNEnv)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", c.Database.DSNEnv)
	}
	return v, nil
}
