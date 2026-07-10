# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Multi-stage build: compile the binary in a Go builder image, then ship
# only the static binary on a distroless base. The final image has no
# shell, no package manager, and no API key baked in.

ARG GO_VERSION=1.25
# IMAGE_VERSION is stamped by the release workflow (release-production.yml) so
# the OCI label `org.opencontainers.image.version` matches the git tag pushed.
# Defaults to `dev` for local builds.
ARG IMAGE_VERSION=dev
ARG IMAGE_REVISION=unknown

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

# The poller module depends on several local replace() targets via the
# workspace, so we copy the relevant slice of the monorepo. Build context
# at `docker build` time should be the repo root.
COPY go.work ./
ENV GOWORK=off
COPY apps/worker/usage-poller/openai/ ./apps/worker/usage-poller/openai/
COPY packages/bus-client/go/ ./packages/bus-client/go/
COPY packages/contracts/telemetry/go/ ./packages/contracts/telemetry/go/
COPY packages/contracts/metrics/go/ ./packages/contracts/metrics/go/
COPY packages/telemetry/schema-lint/go/ ./packages/telemetry/schema-lint/go/
COPY packages/extensions/go/ ./packages/extensions/go/
COPY packages/telemetry/go/ ./packages/telemetry/go/

WORKDIR /src/apps/worker/usage-poller/openai
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/openai-poller \
    ./cmd/openai-poller

FROM gcr.io/distroless/static-debian12:nonroot

# OCI image labels — surfaced by registries (GHCR, Docker Hub) and consumed
# by SBOM / vulnerability scanners. Keep in lockstep with the LICENSE and
# repository URL.
LABEL org.opencontainers.image.title="OpenLLM Metrics OpenAI Poller" \
      org.opencontainers.image.description="OpenAI usage and cost poller (F009) for OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/apps/worker/usage-poller/openai/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

USER nonroot:nonroot

COPY --from=builder /out/openai-poller /usr/local/bin/openai-poller

# Default config path; override with --config at runtime.
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/openai-poller"]
CMD ["--config", "/etc/openllm-poller/openai.yaml"]
