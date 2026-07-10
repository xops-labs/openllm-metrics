// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the OpenAI usage-poller configuration.
//
// The schema matches F009 §9. Values can be overridden by environment
// variables; secrets (API keys) are NEVER read from the YAML file — only
// from the OS environment via the configured `api_key_env` key name.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level config object the poller binary loads.
//
// Field order intentionally mirrors the YAML order in F009 §9 so operators
// can read the YAML and the struct side-by-side.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Providers ProvidersConfig `yaml:"providers"`
	Bus       BusConfig       `yaml:"bus"`
	Labels    Labels          `yaml:"labels"`
}

// ServerConfig configures the HTTP surface (health + Prometheus /metrics).
type ServerConfig struct {
	// Port is the TCP port the metrics + health server binds to. Default 8080.
	Port int `yaml:"port"`
}

// ProvidersConfig is keyed by provider name. F009 ships only `openai`; later
// pollers (F013-F016) each get their own binary and never share a config.
type ProvidersConfig struct {
	OpenAI OpenAIConfig `yaml:"openai"`
}

// OpenAIConfig holds OpenAI-specific settings. The API key never lives in
// this struct — only the name of the env var to read it from.
type OpenAIConfig struct {
	// Enabled toggles polling without rebuilding the binary.
	Enabled bool `yaml:"enabled"`
	// APIKeyEnv is the name of the environment variable holding the
	// OpenAI Admin API key (e.g. "OPENAI_ADMIN_API_KEY"). The actual key
	// is never serialized into config and never logged.
	APIKeyEnv string `yaml:"api_key_env"`
	// PollingIntervalSeconds is the wake interval. Default 300s per vision MVP.
	PollingIntervalSeconds int `yaml:"polling_interval_seconds"`
	// BaseURL overrides the OpenAI endpoint (default
	// "https://api.openai.com"). Useful for staging proxies and tests.
	BaseURL string `yaml:"base_url"`
	// CircuitBreakerThreshold is the number of consecutive 5xx / network
	// failures required before the breaker opens. Default 5.
	CircuitBreakerThreshold int `yaml:"circuit_breaker_threshold"`
	// CircuitBreakerCooldownSeconds is how long the breaker stays open
	// before allowing a probe request. Default 60s.
	CircuitBreakerCooldownSeconds int `yaml:"circuit_breaker_cooldown_seconds"`
	// MaxRetries bounds the exponential-backoff retry budget per request.
	// Default 4.
	MaxRetries int `yaml:"max_retries"`
	// DedupCacheSize bounds the in-memory LRU dedup cache. Default 4096
	// entries (~ days of windows for normal traffic).
	DedupCacheSize int `yaml:"dedup_cache_size"`
}

// BusConfig configures the streaming bus producer. Brokers may be a single
// host:port (local-dev Redpanda) or a comma-separated list.
type BusConfig struct {
	Brokers  []string `yaml:"brokers"`
	ClientID string   `yaml:"client_id"`
}

// Labels are static labels every emitted event/metric inherits. `tenant` is
// mandatory and validated at load time per F005.
type Labels struct {
	Env     string `yaml:"env"`
	Team    string `yaml:"team"`
	Tenant  string `yaml:"tenant"`
	App     string `yaml:"app"`
	Project string `yaml:"project"`
	Region  string `yaml:"region"`
}

// ErrInvalidConfig is returned by Load / Validate when the loaded config
// violates an invariant (missing tenant, bad interval, …).
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads a YAML file from disk, applies defaults, and validates the
// result. It does NOT read the OpenAI API key — call OpenAIAPIKey() on the
// returned config to do so at the moment of use.
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
		c.Server.Port = 8080
	}
	if c.Providers.OpenAI.PollingIntervalSeconds == 0 {
		c.Providers.OpenAI.PollingIntervalSeconds = 300
	}
	if c.Providers.OpenAI.BaseURL == "" {
		c.Providers.OpenAI.BaseURL = "https://api.openai.com"
	}
	if c.Providers.OpenAI.CircuitBreakerThreshold == 0 {
		c.Providers.OpenAI.CircuitBreakerThreshold = 5
	}
	if c.Providers.OpenAI.CircuitBreakerCooldownSeconds == 0 {
		c.Providers.OpenAI.CircuitBreakerCooldownSeconds = 60
	}
	if c.Providers.OpenAI.MaxRetries == 0 {
		c.Providers.OpenAI.MaxRetries = 4
	}
	if c.Providers.OpenAI.DedupCacheSize == 0 {
		c.Providers.OpenAI.DedupCacheSize = 4096
	}
	if c.Providers.OpenAI.APIKeyEnv == "" {
		c.Providers.OpenAI.APIKeyEnv = "OPENAI_ADMIN_API_KEY"
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-openai-poller"
	}
}

// Validate enforces invariants the poller cannot run safely without.
func (c *Config) Validate() error {
	if c.Providers.OpenAI.Enabled && c.Providers.OpenAI.PollingIntervalSeconds <= 0 {
		return fmt.Errorf("%w: providers.openai.polling_interval_seconds must be > 0", ErrInvalidConfig)
	}
	if c.Providers.OpenAI.Enabled && c.Providers.OpenAI.APIKeyEnv == "" {
		return fmt.Errorf("%w: providers.openai.api_key_env must be set", ErrInvalidConfig)
	}
	if c.Labels.Tenant == "" {
		// Per F005, every emitted event must carry a tenant.
		return fmt.Errorf("%w: labels.tenant is required", ErrInvalidConfig)
	}
	if c.Labels.Env == "" {
		return fmt.Errorf("%w: labels.env is required", ErrInvalidConfig)
	}
	switch c.Labels.Env {
	case "development", "staging", "production":
		// ok — matches schema enum
	default:
		return fmt.Errorf("%w: labels.env must be development|staging|production, got %q", ErrInvalidConfig, c.Labels.Env)
	}
	if c.Labels.Team == "" {
		return fmt.Errorf("%w: labels.team is required", ErrInvalidConfig)
	}
	return nil
}

// PollingInterval returns the polling interval as a time.Duration.
func (c *Config) PollingInterval() time.Duration {
	return time.Duration(c.Providers.OpenAI.PollingIntervalSeconds) * time.Second
}

// CircuitBreakerCooldown returns the breaker cooldown as a time.Duration.
func (c *Config) CircuitBreakerCooldown() time.Duration {
	return time.Duration(c.Providers.OpenAI.CircuitBreakerCooldownSeconds) * time.Second
}

// OpenAIAPIKey reads the OpenAI Admin API key from the env var named by
// providers.openai.api_key_env. Returns ("", error) when the env var is
// unset or empty so the caller can fail fast.
//
// The returned key MUST be treated as sensitive: never log it, never echo it,
// never put it in an error message, never persist it. Mask with MaskAPIKey
// before including any reference in a log line.
func (c *Config) OpenAIAPIKey() (string, error) {
	name := c.Providers.OpenAI.APIKeyEnv
	if name == "" {
		return "", fmt.Errorf("%w: api_key_env name not configured", ErrInvalidConfig)
	}
	v := os.Getenv(name)
	if v == "" {
		// Intentionally include the env-var NAME but never any portion of the value.
		return "", fmt.Errorf("config: env var %q is unset or empty", name)
	}
	return v, nil
}

// MaskAPIKey returns a redacted form safe for logs (first 4 + "***" + last 2
// for keys >= 12 chars; full "***" otherwise). Use ONLY for diagnostics; the
// returned form is still considered low-sensitivity and should not be logged
// at INFO level unless explicitly required.
func MaskAPIKey(key string) string {
	if len(key) < 12 {
		return "***"
	}
	return key[:4] + "***" + key[len(key)-2:]
}
