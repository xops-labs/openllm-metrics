// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

module github.com/yasvanth511/openllm-metrics-oss/cmd/olm-audit

go 1.25.0

require github.com/jackc/pgx/v5 v5.10.0

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace github.com/yasvanth511/openllm-metrics-oss/apps/api/audit-service => ../../apps/api/audit-service

replace github.com/yasvanth511/openllm-metrics-oss/packages/bus-client/go => ../../packages/bus-client/go
