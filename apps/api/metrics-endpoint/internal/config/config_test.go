// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package config

import (
	"errors"
	"strings"
	"testing"
)

func TestParse_AppliesDefaults(t *testing.T) {
	cfg, err := Parse([]byte(`
bus:
  brokers: ["localhost:9092"]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Server.Port != 9090 {
		t.Errorf("default port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Bus.ClientID == "" {
		t.Errorf("client_id default missing")
	}
	if cfg.Bus.Group == "" {
		t.Errorf("group default missing")
	}
	if len(cfg.Bus.Topics) != 2 {
		t.Errorf("default topics len=%d, want 2", len(cfg.Bus.Topics))
	}
	if cfg.Replay.WindowHours != 168 {
		t.Errorf("default replay window=%d, want 168", cfg.Replay.WindowHours)
	}
}

func TestParse_RejectsMissingBrokers(t *testing.T) {
	_, err := Parse([]byte(`server: {port: 9090}`))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
	if !strings.Contains(err.Error(), "brokers") {
		t.Errorf("error should mention brokers: %v", err)
	}
}

func TestParse_RejectsBadPort(t *testing.T) {
	_, err := Parse([]byte(`
server:
  port: 99999
bus:
  brokers: ["localhost:9092"]
`))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestParse_RejectsNegativeReplayWindow(t *testing.T) {
	_, err := Parse([]byte(`
bus:
  brokers: ["localhost:9092"]
replay:
  window_hours: -1
`))
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
