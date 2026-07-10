// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

module github.com/yasvanth511/openllm-metrics-oss/packages/telemetry/schema-lint/go

go 1.22

require (
	github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go v0.0.0
	github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go v0.0.0
)

replace github.com/yasvanth511/openllm-metrics-oss/packages/contracts/metrics/go => ../../../contracts/metrics/go

replace github.com/yasvanth511/openllm-metrics-oss/packages/contracts/telemetry/go => ../../../contracts/telemetry/go
