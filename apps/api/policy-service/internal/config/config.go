// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the policy-service configuration.
//
// The policy-service is the OSS-safe data layer for F029: it owns the policy
// document schema, storage, versioning, and CRUD surface. It does NOT evaluate
// policies or make enforcement decisions — that is F030 and lives in the
// this repository.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level config object the policy-service binary loads.
type Config struct {
	Server ServerConfig `yaml:"server"`
	DB     DBConfig     `yaml:"db"`
	Bus    BusConfig    `yaml:"bus"`
	Schema SchemaConfig `yaml:"schema"`
}

// ServerConfig configures the HTTP surface for the CRUD API.
type ServerConfig struct {
	// Port is the TCP port the server binds to. Default 8090.
	Port int `yaml:"port"`
}

// DBConfig holds Postgres connection settings.
type DBConfig struct {
	// DSN is a libpq-format connection string (or postgres:// URL).
	DSN string `yaml:"dsn"`
	// DSNEnv, when set, names an environment variable holding the DSN. It is
	// consulted only when DSN is empty, so deployments can keep the connection
	// string (and its credentials) out of the config file — matching the
	// audit/decision/notifier services' `dsn_env` convention.
	DSNEnv string `yaml:"dsn_env"`
	// MaxOpenConns caps the pool size. Default 10.
	MaxOpenConns int `yaml:"max_open_conns"`
}

// BusConfig configures the audit-event producer.
type BusConfig struct {
	// Brokers is the list of Kafka / Redpanda broker addresses.
	Brokers []string `yaml:"brokers"`
	// ClientID identifies this producer.
	ClientID string `yaml:"client_id"`
	// AuditTopic is the topic audit.event.v1 messages are published to.
	AuditTopic string `yaml:"audit_topic"`
	// Enabled toggles the producer. When false, mutations are still
	// persisted but no audit event is emitted (useful for local dev).
	Enabled bool `yaml:"enabled"`
}

// SchemaConfig points at the JSON Schema used to validate policy documents.
type SchemaConfig struct {
	// Path is the filesystem path to the policy JSON Schema. Defaults to the
	// in-repo contract path so a default deploy works without configuration.
	Path string `yaml:"path"`
}

// ErrInvalidConfig is returned when the loaded configuration violates an
// invariant.
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
	// Resolve the DSN from the named env var when no literal DSN is set, so the
	// connection string can stay out of the committed config file.
	if strings.TrimSpace(c.DB.DSN) == "" && strings.TrimSpace(c.DB.DSNEnv) != "" {
		c.DB.DSN = os.Getenv(c.DB.DSNEnv)
	}
	if c.DB.MaxOpenConns == 0 {
		c.DB.MaxOpenConns = 10
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-policy-service"
	}
	if c.Bus.AuditTopic == "" {
		c.Bus.AuditTopic = "audit.event.v1"
	}
	if c.Schema.Path == "" {
		c.Schema.Path = "packages/contracts/policy/v1/policy.schema.json"
	}
}

// Validate enforces invariants the service cannot run safely without.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("%w: server.port must be 1..65535, got %d", ErrInvalidConfig, c.Server.Port)
	}
	if strings.TrimSpace(c.DB.DSN) == "" {
		return fmt.Errorf("%w: db.dsn must be set", ErrInvalidConfig)
	}
	if c.DB.MaxOpenConns < 1 {
		return fmt.Errorf("%w: db.max_open_conns must be >= 1", ErrInvalidConfig)
	}
	if c.Bus.Enabled && len(c.Bus.Brokers) == 0 {
		return fmt.Errorf("%w: bus.brokers required when bus.enabled is true", ErrInvalidConfig)
	}
	if strings.TrimSpace(c.Schema.Path) == "" {
		return fmt.Errorf("%w: schema.path must be set", ErrInvalidConfig)
	}
	return nil
}
