// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// llmproviderreceiver is a standalone OTel Collector contrib component. It is
// included in the top-level go.work file so workspace-wide builds and CI cover
// it, but the OpenTelemetry Collector Builder (ocb) consumes the module
// independently (via the replaces entry in builder-config.yaml) when
// assembling a custom Collector distribution, so this module keeps its own
// complete go.sum.
module github.com/yasvanth511/openllm-metrics-oss/platform/otel-collector/receiver/llmproviderreceiver

go 1.25.0

require (
	github.com/twmb/franz-go v1.21.0
	go.opentelemetry.io/collector/component v0.110.0
	go.opentelemetry.io/collector/consumer v0.110.0
	go.opentelemetry.io/collector/pdata v1.16.0
	go.opentelemetry.io/collector/receiver v0.110.0
	go.uber.org/zap v1.27.0
)

require (
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	go.opentelemetry.io/collector/config/configtelemetry v0.110.0 // indirect
	go.opentelemetry.io/collector/consumer/consumerprofiles v0.110.0 // indirect
	go.opentelemetry.io/collector/internal/globalsignal v0.110.0 // indirect
	go.opentelemetry.io/collector/pdata/pprofile v0.110.0 // indirect
	go.opentelemetry.io/collector/pipeline v0.110.0 // indirect
	go.opentelemetry.io/otel v1.30.0 // indirect
	go.opentelemetry.io/otel/metric v1.30.0 // indirect
	go.opentelemetry.io/otel/trace v1.30.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.52.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240822170219-fc7c04adadcd // indirect
	google.golang.org/grpc v1.66.2 // indirect
	google.golang.org/protobuf v1.34.2 // indirect
)
