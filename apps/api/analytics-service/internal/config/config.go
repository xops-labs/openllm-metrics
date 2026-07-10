// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the analytics-service configuration.
//
// The analytics-service is the OSS-safe data layer for F038 Phase 3: it owns
// the storage and CRUD surface for per-tenant "saved analytics views". It does
// NOT execute queries, score series, route requests, or apply anomaly rules —
// those behaviors are custom. A saved view is a declarative llm_* selector
// spec (metric, groupBy, filters, wrap, windowSeconds, viz) persisted as JSONB.
package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the top-level config object the analytics-service binary loads.
type Config struct {
	Server ServerConfig `yaml:"server"`
	DB     DBConfig     `yaml:"db"`
}

// ServerConfig configures the HTTP surface for the CRUD API.
type ServerConfig struct {
	// Port is the TCP port the server binds to. Default 8095.
	Port int `yaml:"port"`
}

// DBConfig holds Postgres connection settings.
type DBConfig struct {
	// DSN is a libpq-format connection string (or postgres:// URL).
	DSN string `yaml:"dsn"`
	// DSNEnv, when set, names an environment variable holding the DSN. It is
	// consulted only when DSN is empty, so deployments can keep the connection
	// string (and its credentials) out of the config file — matching the
	// policy/audit/decision services' `dsn_env` convention.
	DSNEnv string `yaml:"dsn_env"`
	// MaxOpenConns caps the pool size. Default 10.
	MaxOpenConns int `yaml:"max_open_conns"`
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
		c.Server.Port = 8095
	}
	// Resolve the DSN from the named env var when no literal DSN is set, so the
	// connection string can stay out of the committed config file.
	if strings.TrimSpace(c.DB.DSN) == "" && strings.TrimSpace(c.DB.DSNEnv) != "" {
		c.DB.DSN = os.Getenv(c.DB.DSNEnv)
	}
	if c.DB.MaxOpenConns == 0 {
		c.DB.MaxOpenConns = 10
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
	return nil
}
