// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package config loads and validates the gateway service configuration.
//
// Configuration is YAML-driven (paths, ports, defaults) with a small set of
// environment overrides for the per-provider upstream URLs so operators can
// repoint a deployment at a region-specific Azure endpoint or a private
// Bedrock VPC endpoint without rebuilding the image. Provider API keys are
// NEVER read here — the gateway forwards the inbound `Authorization` header
// untouched. See README §"Security posture".
package config

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Env-var names used to override the per-provider upstream URLs. F018 §9
// requires per-provider upstreams to be configurable (Azure region, AWS
// endpoint, custom host).
const (
	EnvUpstreamOpenAI      = "OLM_UPSTREAM_OPENAI_URL"
	EnvUpstreamAnthropic   = "OLM_UPSTREAM_ANTHROPIC_URL"
	EnvUpstreamGemini      = "OLM_UPSTREAM_GEMINI_URL"
	EnvUpstreamBedrock     = "OLM_UPSTREAM_BEDROCK_URL"
	EnvUpstreamAzureOpenAI = "OLM_UPSTREAM_AZURE_OPENAI_URL"
)

// Default upstream URLs. Azure has no global default (deployment URL is
// per-tenant) — operators MUST set OLM_UPSTREAM_AZURE_OPENAI_URL or the
// YAML override to use the Azure routes.
const (
	DefaultUpstreamOpenAI    = "https://api.openai.com"
	DefaultUpstreamAnthropic = "https://api.anthropic.com"
	DefaultUpstreamGemini    = "https://generativelanguage.googleapis.com"
	DefaultUpstreamBedrock   = "https://bedrock-runtime.us-east-1.amazonaws.com"
)

// Config is the top-level configuration object.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Upstreams UpstreamsConfig `yaml:"upstreams"`
	Bus       BusConfig       `yaml:"bus"`
	Defaults  DefaultLabels   `yaml:"defaults"`
}

// ServerConfig configures the gateway's HTTP surface.
type ServerConfig struct {
	// Port is the public proxy port. Default 8080.
	Port int `yaml:"port"`
	// MetricsPort exposes /metrics, /healthz, /readyz on a side channel so
	// the proxy port stays pure-proxy. Default 8081.
	MetricsPort int `yaml:"metrics_port"`
	// ReadHeaderTimeoutSeconds bounds the slow-loris exposure on the proxy
	// listener. Default 10.
	ReadHeaderTimeoutSeconds int `yaml:"read_header_timeout_seconds"`
	// UpstreamTimeoutSeconds is the round-trip deadline against the
	// provider. 0 = no deadline; streaming responses set their own deadline
	// via context. Default 0 (let the client/provider decide).
	UpstreamTimeoutSeconds int `yaml:"upstream_timeout_seconds"`
}

// UpstreamsConfig holds per-provider upstream base URLs. Values from the
// matching env vars override the YAML.
type UpstreamsConfig struct {
	OpenAI      string `yaml:"openai"`
	Anthropic   string `yaml:"anthropic"`
	Gemini      string `yaml:"gemini"`
	Bedrock     string `yaml:"bedrock"`
	AzureOpenAI string `yaml:"azure_openai"`
}

// BusConfig configures the streaming-bus producer used to publish runtime
// events. If Brokers is empty the gateway runs in "no-bus" mode (events are
// dropped after metrics increment); useful for local smoke tests.
type BusConfig struct {
	Brokers  []string `yaml:"brokers"`
	ClientID string   `yaml:"client_id"`
}

// DefaultLabels are the fallback values applied when the inbound
// X-OLM-* headers are absent. The gateway is multi-tenant from day one
// (F018 §11): tenant + team + env are mandatory and rejecting a request
// when they are missing is a deliberate design choice — but for the OSS
// out-of-box experience we honor configured defaults instead of returning
// 400.
type DefaultLabels struct {
	Tenant  string `yaml:"tenant"`
	Team    string `yaml:"team"`
	App     string `yaml:"app"`
	Env     string `yaml:"env"`
	Project string `yaml:"project"`
}

// ErrInvalidConfig is returned by Load / Validate when the loaded config
// violates an invariant.
var ErrInvalidConfig = errors.New("config: invalid configuration")

// Load reads a YAML file from disk, applies defaults + env overrides, and
// validates.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	return Parse(raw)
}

// Parse decodes YAML bytes, applies defaults + env overrides, and validates.
func Parse(raw []byte) (*Config, error) {
	var cfg Config
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return nil, fmt.Errorf("config: parse yaml: %w", err)
		}
	}
	cfg.applyDefaults()
	cfg.applyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.MetricsPort == 0 {
		c.Server.MetricsPort = 8081
	}
	if c.Server.ReadHeaderTimeoutSeconds == 0 {
		c.Server.ReadHeaderTimeoutSeconds = 10
	}
	if c.Upstreams.OpenAI == "" {
		c.Upstreams.OpenAI = DefaultUpstreamOpenAI
	}
	if c.Upstreams.Anthropic == "" {
		c.Upstreams.Anthropic = DefaultUpstreamAnthropic
	}
	if c.Upstreams.Gemini == "" {
		c.Upstreams.Gemini = DefaultUpstreamGemini
	}
	if c.Upstreams.Bedrock == "" {
		c.Upstreams.Bedrock = DefaultUpstreamBedrock
	}
	if c.Bus.ClientID == "" {
		c.Bus.ClientID = "openllm-gateway"
	}
	if c.Defaults.Tenant == "" {
		c.Defaults.Tenant = "default"
	}
	if c.Defaults.Team == "" {
		c.Defaults.Team = "unknown"
	}
	if c.Defaults.Env == "" {
		c.Defaults.Env = "development"
	}
}

func (c *Config) applyEnvOverrides() {
	if v := os.Getenv(EnvUpstreamOpenAI); v != "" {
		c.Upstreams.OpenAI = v
	}
	if v := os.Getenv(EnvUpstreamAnthropic); v != "" {
		c.Upstreams.Anthropic = v
	}
	if v := os.Getenv(EnvUpstreamGemini); v != "" {
		c.Upstreams.Gemini = v
	}
	if v := os.Getenv(EnvUpstreamBedrock); v != "" {
		c.Upstreams.Bedrock = v
	}
	if v := os.Getenv(EnvUpstreamAzureOpenAI); v != "" {
		c.Upstreams.AzureOpenAI = v
	}
}

// Validate enforces invariants the gateway cannot run safely without.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("%w: server.port must be 1..65535, got %d", ErrInvalidConfig, c.Server.Port)
	}
	if c.Server.MetricsPort <= 0 || c.Server.MetricsPort > 65535 {
		return fmt.Errorf("%w: server.metrics_port must be 1..65535, got %d", ErrInvalidConfig, c.Server.MetricsPort)
	}
	if c.Server.MetricsPort == c.Server.Port {
		return fmt.Errorf("%w: server.metrics_port must differ from server.port", ErrInvalidConfig)
	}
	if c.Server.ReadHeaderTimeoutSeconds < 0 {
		return fmt.Errorf("%w: server.read_header_timeout_seconds must be >= 0", ErrInvalidConfig)
	}
	if c.Server.UpstreamTimeoutSeconds < 0 {
		return fmt.Errorf("%w: server.upstream_timeout_seconds must be >= 0", ErrInvalidConfig)
	}
	switch c.Defaults.Env {
	case "development", "staging", "production":
	default:
		return fmt.Errorf("%w: defaults.env must be development|staging|production, got %q", ErrInvalidConfig, c.Defaults.Env)
	}
	return nil
}
