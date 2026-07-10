// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the FOCUS-ingester configuration.
//
// The ingester polls the upstream llm-usage-exporter `/focus.json` endpoint
// on a slow cadence (default 1 hour), writes the raw FOCUS line items into
// control_plane.focus_records, and emits llm.usage.reconciled events to
// the bus with the canonical tenant labels resolved from
// control_plane.label_mappings.
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
	Server   ServerConfig   `yaml:"server"`
	Focus    FocusConfig    `yaml:"focus"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
	Defaults DefaultLabels  `yaml:"defaults"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz).
type ServerConfig struct {
	// Port the metrics + health server binds to. Default 8082.
	Port int `yaml:"port"`
}

// FocusConfig points at the upstream /focus.json endpoint.
type FocusConfig struct {
	// URL is the upstream /focus.json endpoint.
	URL string `yaml:"url"`
	// PollIntervalSeconds is how often the ingester polls. Default 3600s.
	// /focus.json is a billing snapshot — frequent polls do not produce
	// fresher data and only stress the upstream.
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// PollTimeoutSeconds bounds each HTTP fetch. Default 30s.
	PollTimeoutSeconds int `yaml:"poll_timeout_seconds"`
}

// DatabaseConfig holds the control-plane Postgres connection.
type DatabaseConfig struct {
	// DSNEnv is the environment variable name holding the Postgres DSN.
	DSNEnv string `yaml:"dsn_env"`
	// MappingCacheTTLSeconds is how long a positive mapping lookup is cached.
	MappingCacheTTLSeconds int `yaml:"mapping_cache_ttl_seconds"`
}

// BusConfig configures the streaming bus producer.
type BusConfig struct {
	Brokers  []string `yaml:"brokers"`
	ClientID string   `yaml:"client_id"`
}

// DefaultLabels pin the tenant/env labels stamped on the worker's own
// metrics exposition. Unmapped FOCUS records are never labeled with these —
// they are counted and dropped until operators seed label_mappings.
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
		c.Server.Port = 8082
	}
	if c.Focus.PollIntervalSeconds == 0 {
		c.Focus.PollIntervalSeconds = 3600
	}
	if c.Focus.PollTimeoutSeconds == 0 {
		c.Focus.PollTimeoutSeconds = 30
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_CONTROL_PLANE_DSN"
	}
	if c.Database.MappingCacheTTLSeconds == 0 {
		c.Database.MappingCacheTTLSeconds = 300
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-focus-ingester"
	}
}

// Validate enforces invariants the ingester cannot run safely without.
func (c *Config) Validate() error {
	if c.Focus.URL == "" {
		return fmt.Errorf("%w: focus.url is required", ErrInvalidConfig)
	}
	if c.Focus.PollIntervalSeconds <= 0 {
		return fmt.Errorf("%w: focus.poll_interval_seconds must be > 0", ErrInvalidConfig)
	}
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	if c.Defaults.Tenant == "" {
		return fmt.Errorf("%w: defaults.tenant is required", ErrInvalidConfig)
	}
	switch c.Defaults.Env {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("%w: defaults.env must be development|staging|production, got %q", ErrInvalidConfig, c.Defaults.Env)
	}
	return nil
}

// PollInterval returns the poll interval as a Duration.
func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.Focus.PollIntervalSeconds) * time.Second
}

// PollTimeout returns the poll timeout as a Duration.
func (c *Config) PollTimeout() time.Duration {
	return time.Duration(c.Focus.PollTimeoutSeconds) * time.Second
}

// MappingCacheTTL returns the mapping-cache TTL as a Duration.
func (c *Config) MappingCacheTTL() time.Duration {
	return time.Duration(c.Database.MappingCacheTTLSeconds) * time.Second
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
func (c *Config) DSN() (string, error) {
	v := os.Getenv(c.Database.DSNEnv)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", c.Database.DSNEnv)
	}
	return v, nil
}
