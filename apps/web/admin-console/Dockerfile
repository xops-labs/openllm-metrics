# Copyright 2026 Yasvanth Udayakumar
# Licensed under the Apache License, Version 2.0.
#
# Next.js standalone build for the OSS admin console (F032).
#
# Build context must be the REPO ROOT so the pnpm workspace (pnpm-lock.yaml,
# pnpm-workspace.yaml, the shared packages/*) resolves. Example:
#
#   docker build -f apps/web/admin-console/Dockerfile -t openllm/admin-console:dev .
#   docker run --rm -p 3000:3000 openllm/admin-console:dev
#
# The image runs `next start` against the standalone server bundle emitted by
# `output: 'standalone'` (see next.config.mjs). No prompts or completions are
# ever rendered by this app; it talks only to control-plane services and the
# Prometheus-compatible query API.

ARG NODE_VERSION=20
ARG IMAGE_VERSION=dev
ARG IMAGE_REVISION=unknown

# ---- builder: install workspace deps and compile the standalone bundle ----
# Install and build in a SINGLE stage. Copying pnpm's node_modules across stages
# breaks the symlinks into the hoisted `.pnpm` store (next is symlinked from the
# app's node_modules), so the build must run in the same filesystem as install.
FROM node:${NODE_VERSION}-alpine AS builder
RUN corepack enable
WORKDIR /repo

# Workspace manifests first for layer caching.
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/admin-console/package.json ./apps/web/admin-console/package.json

# Install only the admin-console workspace and its deps. Allow lockfile drift in
# the image build (the OSS root lockfile is owned outside this Dockerfile's
# scope); production images pin via the committed lockfile.
RUN pnpm install --filter @openllm-metrics/admin-console... --no-frozen-lockfile

# App source after deps for layer caching.
COPY apps/web/admin-console ./apps/web/admin-console

# NEXT_OUTPUT=standalone opts into the self-contained server bundle (see
# next.config.mjs). Tracing uses symlinks, which work on Linux.
ENV NEXT_TELEMETRY_DISABLED=1 \
    NEXT_OUTPUT=standalone
RUN pnpm --filter @openllm-metrics/admin-console build

# ---- runner: minimal runtime image ----
FROM node:${NODE_VERSION}-alpine AS runner
WORKDIR /app

LABEL org.opencontainers.image.title="OpenLLM Metrics Admin Console" \
      org.opencontainers.image.description="OSS admin and governance console (F032) for OpenLLM Metrics." \
      org.opencontainers.image.source="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.url="https://github.com/yasvanth511/openllm-metrics-oss" \
      org.opencontainers.image.documentation="https://github.com/yasvanth511/openllm-metrics-oss/blob/main/apps/web/admin-console/README.md" \
      org.opencontainers.image.vendor="OpenLLM Metrics" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${IMAGE_VERSION}" \
      org.opencontainers.image.revision="${IMAGE_REVISION}"

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3000 \
    HOSTNAME=0.0.0.0

# Run as the built-in non-root `node` user.
USER node

# Standalone output: server.js + a trimmed node_modules, plus static assets.
COPY --from=builder --chown=node:node /repo/apps/web/admin-console/.next/standalone ./
COPY --from=builder --chown=node:node /repo/apps/web/admin-console/.next/static ./apps/web/admin-console/.next/static
COPY --from=builder --chown=node:node /repo/apps/web/admin-console/public ./apps/web/admin-console/public

EXPOSE 3000

# The standalone bundle places server.js under the workspace path.
CMD ["node", "apps/web/admin-console/server.js"]
