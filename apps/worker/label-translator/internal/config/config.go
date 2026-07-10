// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the label-translator configuration.
//
// The translator's job: scrape the upstream llm-usage-exporter /metrics
// endpoint, enrich each sample with a canonical {tenant, team, app, env,
// project} tuple from the control_plane.label_mappings table, and publish
// translated events to the streaming bus with source=exporter.
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
	Exporter ExporterConfig `yaml:"exporter"`
	Database DatabaseConfig `yaml:"database"`
	Bus      BusConfig      `yaml:"bus"`
	Defaults DefaultLabels  `yaml:"defaults"`
}

// ServerConfig configures the HTTP surface (/metrics, /healthz).
type ServerConfig struct {
	// Port is the TCP port the metrics + health server binds to. Default 8081.
	Port int `yaml:"port"`
}

// ExporterConfig points at the upstream llm-usage-exporter scrape target.
type ExporterConfig struct {
	// URL is the upstream /metrics endpoint (e.g. http://llm-usage-exporter:9090/metrics).
	URL string `yaml:"url"`
	// ScrapeIntervalSeconds is how often the translator scrapes the upstream
	// exporter. Must align with the exporter's own poll cadence to avoid
	// double-counting; default 60s.
	ScrapeIntervalSeconds int `yaml:"scrape_interval_seconds"`
	// ScrapeTimeoutSeconds bounds each scrape. Default 15s.
	ScrapeTimeoutSeconds int `yaml:"scrape_timeout_seconds"`
}

// DatabaseConfig holds the control-plane Postgres connection. The DSN is
// expected to be supplied via the env var named by DSNEnv so connection
// strings (which may include credentials) never live in the YAML file.
type DatabaseConfig struct {
	// DSNEnv is the environment variable name holding the Postgres DSN.
	DSNEnv string `yaml:"dsn_env"`
	// MappingCacheTTLSeconds is how long a positive mapping lookup is cached
	// in-process. Default 300s. Cache invalidation on schema mutation is
	// owned by the admin console (F032); this TTL is a safety bound.
	MappingCacheTTLSeconds int `yaml:"mapping_cache_ttl_seconds"`
}

// BusConfig configures the streaming bus producer.
type BusConfig struct {
	Brokers  []string `yaml:"brokers"`
	ClientID string   `yaml:"client_id"`
}

// DefaultLabels are the fallback labels emitted when label_mappings has no
// row for an inbound (provider, tenant_external_id, tenancy_id) tuple.
// The translator still emits an event in this case so dashboards do not
// silently lose data; it also bumps llm_label_translation_unmapped_total
// so an alert can fire on a sustained gap.
type DefaultLabels struct {
	// Tenant is the fallback tenant slug. MUST be set; an unmapped event
	// without a tenant violates F005 and is dropped.
	Tenant string `yaml:"tenant"`
	// Team is the fallback team. Default "unknown".
	Team string `yaml:"team"`
	// Env is the fallback environment. Must be one of development|staging|production.
	Env string `yaml:"env"`
}

// ErrInvalidConfig is returned when validation fails.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads YAML from disk, applies defaults, and validates the result.
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
		c.Server.Port = 8081
	}
	if c.Exporter.ScrapeIntervalSeconds == 0 {
		c.Exporter.ScrapeIntervalSeconds = 60
	}
	if c.Exporter.ScrapeTimeoutSeconds == 0 {
		c.Exporter.ScrapeTimeoutSeconds = 15
	}
	if c.Database.DSNEnv == "" {
		c.Database.DSNEnv = "OPENLLM_CONTROL_PLANE_DSN"
	}
	if c.Database.MappingCacheTTLSeconds == 0 {
		c.Database.MappingCacheTTLSeconds = 300
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-label-translator"
	}
	if c.Defaults.Team == "" {
		c.Defaults.Team = "unknown"
	}
}

// Validate enforces invariants the translator cannot run safely without.
func (c *Config) Validate() error {
	if c.Exporter.URL == "" {
		return fmt.Errorf("%w: exporter.url is required", ErrInvalidConfig)
	}
	if c.Exporter.ScrapeIntervalSeconds <= 0 {
		return fmt.Errorf("%w: exporter.scrape_interval_seconds must be > 0", ErrInvalidConfig)
	}
	if len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers must not be empty", ErrInvalidConfig)
	}
	if c.Defaults.Tenant == "" {
		return fmt.Errorf("%w: defaults.tenant is required (drop-safe fallback for unmapped events)", ErrInvalidConfig)
	}
	switch c.Defaults.Env {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("%w: defaults.env must be development|staging|production, got %q", ErrInvalidConfig, c.Defaults.Env)
	}
	return nil
}

// ScrapeInterval returns the configured scrape interval as a Duration.
func (c *Config) ScrapeInterval() time.Duration {
	return time.Duration(c.Exporter.ScrapeIntervalSeconds) * time.Second
}

// ScrapeTimeout returns the configured scrape timeout as a Duration.
func (c *Config) ScrapeTimeout() time.Duration {
	return time.Duration(c.Exporter.ScrapeTimeoutSeconds) * time.Second
}

// MappingCacheTTL returns the mapping-cache TTL as a Duration.
func (c *Config) MappingCacheTTL() time.Duration {
	return time.Duration(c.Database.MappingCacheTTLSeconds) * time.Second
}

// DSN reads the Postgres DSN from the env var named by Database.DSNEnv.
// The value MUST be treated as a secret: never log it.
func (c *Config) DSN() (string, error) {
	name := c.Database.DSNEnv
	v := os.Getenv(name)
	if v == "" {
		return "", fmt.Errorf("config: env var %q is unset or empty", name)
	}
	return v, nil
}
