# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Multi-stage build: compile the binary in a Go builder image, then ship
# only the static binary on a distroless base. The final image has no
# shell, no package manager, and no DSN baked in. Build context at
# `docker build` time must be the repo root.

ARG GO_VERSION=1.25
# IMAGE_VERSION is stamped by the release workflow so the OCI label
# `org.opencontainers.image.version` matches the git tag pushed. Defaults to
# `dev` for local builds.
ARG IMAGE_VERSION=dev
ARG IMAGE_REVISION=unknown

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# analytics-service is a self-contained module (no workspace replace targets),
# so we copy just its slice of the monorepo and build with the workspace off.
ENV GOWORK=off
COPY apps/api/analytics-service/ ./apps/api/analytics-service/

WORKDIR /src/apps/api/analytics-service
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/analytics-service \
    ./cmd/analytics-service

FROM gcr.io/distroless/static-debian12:nonroot

# OCI image labels — surfaced by registries (GHCR, Docker Hub) and consumed
# by SBOM / vulnerability scanners. Keep in lockstep with the LICENSE and
# repository URL.
LABEL org.opencontainers.image.title="OpenLLM Metrics Analytics Service" \
      org.opencontainers.image.description="OSS-safe analytics saved-views CRUD service (F038) — per-tenant dashboard view specs — for OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/apps/api/analytics-service/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

USER nonroot:nonroot

COPY --from=builder /out/analytics-service /usr/local/bin/analytics-service

# /healthz is for orchestrator probes. Default port matches config defaults
# (8095).
EXPOSE 8095

ENTRYPOINT ["/usr/local/bin/analytics-service"]
CMD ["--config", "/etc/openllm-analytics-service/config.yaml"]
