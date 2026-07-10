// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

module github.com/yasvanth511/openllm-metrics-oss/apps/gateway

go 1.25.0

require (
	github.com/google/uuid v1.6.0
	github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go v0.0.0
	github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/twmb/franz-go v1.21.0 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	go.opentelemetry.io/otel v1.30.0 // indirect
	go.opentelemetry.io/otel/metric v1.30.0 // indirect
	go.opentelemetry.io/otel/trace v1.30.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
)

replace github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go => ../../packages/bus-client/go

replace github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go => ../../packages/contracts/telemetry/go
