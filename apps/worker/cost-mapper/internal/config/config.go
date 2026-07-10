// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the cost-mapper configuration.
//
// The cost-mapper subscribes to two bus topics:
//
//   - llm.runtime.normalized — runtime events from the gateway/SDK
//   - llm.usage.reconciled   — FOCUS-derived billing events from the
//     focus-ingester
//
// It publishes one new topic:
//
//   - llm.cost.estimated — token-priced cost estimate events
//
// All other invariants (multi-tenant labels, no payload logging, OTel
// gen_ai.* alignment) are enforced inside the internal packages.
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
	Catalog  CatalogConfig  `yaml:"catalog"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
	Defaults DefaultLabels  `yaml:"defaults"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz).
type ServerConfig struct {
	Port int `yaml:"port"`
}

// CatalogConfig points at the platform/pricing/ directory.
type CatalogConfig struct {
	// Dir is the directory containing the per-provider pricing YAML files.
	Dir string `yaml:"dir"`
	// ReloadIntervalSeconds reloads the catalog on a slow cadence so price
	// PRs land without a restart. 0 disables reloads.
	ReloadIntervalSeconds int `yaml:"reload_interval_seconds"`
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
	// Topics the cost-mapper consumes. The set is fixed but exposed in
	// config so operators can re-route during incident recovery.
	RuntimeTopic    string `yaml:"runtime_topic"`
	ReconciledTopic string `yaml:"reconciled_topic"`
	EstimatedTopic  string `yaml:"estimated_topic"`
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
		c.Server.Port = 8083
	}
	if c.Catalog.Dir == "" {
		c.Catalog.Dir = "platform/pricing"
	}
	if c.Catalog.ReloadIntervalSeconds == 0 {
		c.Catalog.ReloadIntervalSeconds = 300
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_CONTROL_PLANE_DSN"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-cost-mapper"
	}
	if c.Bus.ConsumerGroup == "" {
		c.Bus.ConsumerGroup = "openllm-cost-mapper"
	}
	if c.Bus.RuntimeTopic == "" {
		c.Bus.RuntimeTopic = "llm.runtime.normalized"
	}
	if c.Bus.ReconciledTopic == "" {
		c.Bus.ReconciledTopic = "llm.usage.reconciled"
	}
	if c.Bus.EstimatedTopic == "" {
		c.Bus.EstimatedTopic = "llm.cost.estimated"
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
	return nil
}

// CatalogReloadInterval returns the reload cadence as a Duration.
func (c *Config) CatalogReloadInterval() time.Duration {
	return time.Duration(c.Catalog.ReloadIntervalSeconds) * time.Second
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
func (c *Config) DSN() (string, error) {
	v := os.Getenv(c.Database.DSNEnv)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", c.Database.DSNEnv)
	}
	return v, nil
}
