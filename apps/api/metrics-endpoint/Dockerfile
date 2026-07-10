# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Multi-stage build: compile the binary in a Go builder image, then ship
# only the static binary on a distroless base. The final image has no
# shell, no package manager, and no secrets baked in. Build context at
# `docker build` time must be the repo root so the workspace replace()
# targets resolve.

ARG GO_VERSION=1.25
# IMAGE_VERSION is stamped by the release workflow (release-production.yml) so
# the OCI label `org.opencontainers.image.version` matches the git tag pushed.
# Defaults to `dev` for local builds.
ARG IMAGE_VERSION=dev
ARG IMAGE_REVISION=unknown

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# Copy only the slice of the monorepo the service needs.
COPY go.work ./
ENV GOWORK=off
COPY apps/api/metrics-endpoint/ ./apps/api/metrics-endpoint/
COPY packages/bus-client/go/ ./packages/bus-client/go/
COPY packages/contracts/telemetry/go/ ./packages/contracts/telemetry/go/
COPY packages/contracts/metrics/go/ ./packages/contracts/metrics/go/
COPY packages/telemetry/schema-lint/go/ ./packages/telemetry/schema-lint/go/

WORKDIR /src/apps/api/metrics-endpoint
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/metrics-endpoint \
    ./cmd/metrics-endpoint

FROM gcr.io/distroless/static-debian12:nonroot

# OCI image labels — surfaced by registries (GHCR, Docker Hub) and consumed
# by SBOM / vulnerability scanners. Keep in lockstep with the LICENSE and
# repository URL.
LABEL org.opencontainers.image.title="OpenLLM Metrics /metrics Aggregator" \
      org.opencontainers.image.description="Prometheus /metrics aggregator service (F010) for OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/apps/api/metrics-endpoint/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

USER nonroot:nonroot

COPY --from=builder /out/metrics-endpoint /usr/local/bin/metrics-endpoint

# /metrics is the public scrape surface; /healthz and /readyz are for
# orchestrator probes.
EXPOSE 9090

ENTRYPOINT ["/usr/local/bin/metrics-endpoint"]
CMD ["--config", "/etc/openllm-metrics/metrics-endpoint.yaml"]
