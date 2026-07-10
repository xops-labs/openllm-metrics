# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Multi-stage build: compile the binary in a Go builder image, then ship
# only the static binary on a distroless base. Build context at
# `docker build` time must be the repo root so the workspace replace()
# targets resolve.

ARG GO_VERSION=1.25
ARG IMAGE_VERSION=dev
ARG IMAGE_REVISION=unknown

FROM golang:${GO_VERSION}-alpine AS builder

WORKDIR /src

COPY go.work ./
ENV GOWORK=off
COPY examples/demo-generator/ ./examples/demo-generator/
COPY packages/bus-client/go/ ./packages/bus-client/go/
COPY packages/contracts/telemetry/go/ ./packages/contracts/telemetry/go/

WORKDIR /src/examples/demo-generator
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags "-s -w" \
    -o /out/demo-generator \
    .

FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="OpenLLM Metrics Demo Generator" \
      org.opencontainers.image.description="Synthetic schema-conformant LLM telemetry generator for the no-API-key demo profile of OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/examples/demo-generator/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

USER nonroot:nonroot

COPY --from=builder /out/demo-generator /usr/local/bin/demo-generator

# /healthz with emit counters.
EXPOSE 8089

ENTRYPOINT ["/usr/local/bin/demo-generator"]
