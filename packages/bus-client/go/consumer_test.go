// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package busclient_test

import (
	"context"
	"errors"
	"testing"

	busclient "github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go"
)

// newTestProducer builds a real Producer against an unreachable broker.
// kgo.NewClient does not dial eagerly, and ProduceEvent validates its
// arguments before any network I/O, so these tests need no live bus.
func newTestProducer(t *testing.T) *busclient.Producer {
	t.Helper()
	p, err := busclient.NewProducer(busclient.Config{
		Brokers:  []string{"127.0.0.1:1"},
		ClientID: "test",
	})
	if err != nil {
		t.Fatalf("NewProducer: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestProduceEvent_RequiresTenantID(t *testing.T) {
	t.Parallel()
	p := newTestProducer(t)
	err := p.ProduceEvent(context.Background(), "topic", "event-1", "", nil)
	if !errors.Is(err, busclient.ErrMissingTenantID) {
		t.Fatalf("expected ErrMissingTenantID, got %v", err)
	}
}

func TestProduceEvent_RequiresEventID(t *testing.T) {
	t.Parallel()
	p := newTestProducer(t)
	err := p.ProduceEvent(context.Background(), "topic", "", "tenant-1", nil)
	if !errors.Is(err, busclient.ErrMissingEventID) {
		t.Fatalf("expected ErrMissingEventID, got %v", err)
	}
}
