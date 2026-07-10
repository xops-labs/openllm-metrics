// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

module github.com/yasvanth511/openllm-metrics-oss/apps/api/decision-service

go 1.25.0

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/twmb/franz-go v1.21.0
	github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pierrec/lz4/v4 v4.1.26 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/twmb/franz-go/pkg/kmsg v1.13.1 // indirect
	go.opentelemetry.io/otel v1.30.0 // indirect
	go.opentelemetry.io/otel/metric v1.30.0 // indirect
	go.opentelemetry.io/otel/trace v1.30.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go => ../../../packages/bus-client/go
