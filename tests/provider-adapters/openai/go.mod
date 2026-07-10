// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

module github.com/yasvanth511/openllm-metrics-oss/tests/provider-adapters/openai

go 1.25.0

require (
	github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai v0.0.0
	github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go v0.0.0
	github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go v0.0.0
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/twmb/franz-go v1.21.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go v0.0.0 // indirect
	github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go v0.0.0 // indirect
	go.opentelemetry.io/otel v1.30.0 // indirect
	go.opentelemetry.io/otel/metric v1.30.0 // indirect
	go.opentelemetry.io/otel/trace v1.30.0 // indirect
)

replace github.com/yasvanth511/openllm-metrics-oss/apps/worker/usage-poller/openai => ../../../apps/worker/usage-poller/openai

replace github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go => ../../../packages/bus-client/go

replace github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go => ../../../packages/contracts/metrics/go

replace github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go => ../../../packages/contracts/telemetry/go

replace github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go => ../../../packages/telemetry/schema-lint/go
