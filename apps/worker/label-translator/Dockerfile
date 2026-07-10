# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Multi-stage build: compile the binary in a Go builder image, then ship
# only the static binary on a distroless base. The final image has no
# shell, no package manager, and no DSN baked in. Build context at
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

# The label-translator module depends on the bus-client and telemetry
# contracts packages via the workspace, so we copy the relevant slice of
# the monorepo. Build context at `docker build` time should be the repo root.
COPY go.work ./
ENV GOWORK=off
COPY apps/worker/label-translator/ ./apps/worker/label-translator/
COPY packages/bus-client/go/ ./packages/bus-client/go/
COPY packages/contracts/telemetry/go/ ./packages/contracts/telemetry/go/

WORKDIR /src/apps/worker/label-translator
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/label-translator \
    ./cmd/label-translator

FROM gcr.io/distroless/static-debian12:nonroot

# OCI image labels — surfaced by registries (GHCR, Docker Hub) and consumed
# by SBOM / vulnerability scanners. Keep in lockstep with the LICENSE and
# repository URL.
LABEL org.opencontainers.image.title="OpenLLM Metrics Label Translator" \
      org.opencontainers.image.description="Phase B label-translation worker (scrape upstream exporter, enrich with canonical tenant labels) for OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/apps/worker/label-translator/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

USER nonroot:nonroot

COPY --from=builder /out/label-translator /usr/local/bin/label-translator

# /metrics is the self-observability scrape surface; /healthz is for
# orchestrator probes. Default port matches config defaults (8081).
EXPOSE 8081

ENTRYPOINT ["/usr/local/bin/label-translator"]
CMD ["--config", "/etc/openllm-label-translator/config.yaml"]
