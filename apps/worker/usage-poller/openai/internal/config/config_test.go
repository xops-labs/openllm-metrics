// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package config_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai/internal/config"
)

func TestParse_ValidAppliesDefaults(t *testing.T) {
	t.Parallel()
	raw := []byte(`
providers:
  openai:
    enabled: true
    api_key_env: OPENAI_ADMIN_API_KEY
labels:
  env: production
  team: ai-platform
  tenant: tenant-001
`)
	cfg, err := config.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("default port = %d, want 8080", cfg.Server.Port)
	}
	if cfg.Providers.OpenAI.PollingIntervalSeconds != 300 {
		t.Fatalf("default interval = %d, want 300", cfg.Providers.OpenAI.PollingIntervalSeconds)
	}
	if cfg.Providers.OpenAI.BaseURL != "https://api.openai.com" {
		t.Fatalf("default base_url = %q", cfg.Providers.OpenAI.BaseURL)
	}
}

func TestParse_MissingTenantIsRejected(t *testing.T) {
	t.Parallel()
	raw := []byte(`
providers:
  openai:
    enabled: true
    api_key_env: OPENAI_ADMIN_API_KEY
labels:
  env: production
  team: ai-platform
`)
	_, err := config.Parse(raw)
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if !strings.Contains(err.Error(), "tenant") {
		t.Fatalf("error must mention tenant: %v", err)
	}
}

func TestParse_BadEnvEnumIsRejected(t *testing.T) {
	t.Parallel()
	raw := []byte(`
providers:
  openai:
    enabled: true
    api_key_env: OPENAI_ADMIN_API_KEY
labels:
  env: prod
  team: ai-platform
  tenant: tenant-001
`)
	_, err := config.Parse(raw)
	if !errors.Is(err, config.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestOpenAIAPIKey_ReadsFromEnv(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY", "sk-test-value")
	raw := []byte(`
providers:
  openai:
    enabled: true
    api_key_env: TEST_OPENAI_KEY
labels:
  env: production
  team: ai-platform
  tenant: tenant-001
`)
	cfg, err := config.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	key, err := cfg.OpenAIAPIKey()
	if err != nil {
		t.Fatalf("OpenAIAPIKey: %v", err)
	}
	if key != "sk-test-value" {
		t.Fatalf("got key %q", key)
	}
}

func TestOpenAIAPIKey_UnsetEnvIsAnError(t *testing.T) {
	t.Setenv("TEST_OPENAI_KEY_MISSING", "")
	raw := []byte(`
providers:
  openai:
    enabled: true
    api_key_env: TEST_OPENAI_KEY_MISSING
labels:
  env: production
  team: ai-platform
  tenant: tenant-001
`)
	cfg, err := config.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = cfg.OpenAIAPIKey()
	if err == nil {
		t.Fatal("expected error when env var is unset")
	}
}

func TestMaskAPIKey(t *testing.T) {
	t.Parallel()
	if got := config.MaskAPIKey("sk-very-long-secret-12345678"); got == "sk-very-long-secret-12345678" {
		t.Fatal("mask must not return original")
	}
	if got := config.MaskAPIKey("short"); got != "***" {
		t.Fatalf("short key not redacted: %q", got)
	}
	long := "sk-abcdefghijklmnop"
	got := config.MaskAPIKey(long)
	if strings.Contains(got, "cdefghij") {
		t.Fatalf("middle of key leaked: %q", got)
	}
}
