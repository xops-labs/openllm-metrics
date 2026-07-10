# @openllm/metrics

Node.js runtime instrumentation SDK for [OpenLLM Metrics](https://github.com/yasvanth511/openllm-metrics-oss) — a vendor-neutral telemetry control plane for multi-provider LLM APIs.

The SDK emits OpenTelemetry GenAI semantic-convention metrics (`gen_ai.client.operation.duration`, `gen_ai.client.token.usage`) plus the project-specific `llm_requests_total` and `llm_usage_dollars_total` counters, tagged with multi-tenant context (`tenant`, `team`, `app`, `env`, `project`, `route`) on every operation.

## Privacy invariant

**The SDK never collects prompt or completion text.** Only token counts, durations, error categories, response model IDs, and tenant labels flow through the exporter. This is enforced by the type system — there is no `prompt`/`messages` slot in the SDK's call options.

## Install

> **Not yet published to npm.** Until `@openllm/metrics` is released, consume it
> from source inside this monorepo (it is a pnpm workspace package):
>
> ```bash
> pnpm install
> pnpm --filter @openllm/metrics build
> # then depend on it from another workspace package:
> #   "@openllm/metrics": "workspace:*"
> ```

Once published, installation will be:

```bash
npm install @openllm/metrics
# or
pnpm add @openllm/metrics
```

## 10-line quickstart

```ts
import { init, withLlmCall } from '@openllm/metrics';
import OpenAI from 'openai';

await init({ serviceName: 'rag-svc', exporterEndpoint: 'http://localhost:4318' });
const openai = new OpenAI();

await withLlmCall(
  {
    provider: 'openai',
    model: 'gpt-4o-mini',
    tenant: 'acme',
    team: 'platform-search',
    app: 'rag-svc',
    env: 'prod',
    project: 'acme-search',
  },
  async (op) => {
    const r = await openai.chat.completions.create({
      model: 'gpt-4o-mini',
      messages: [{ role: 'user', content: 'ping' }],
    });
    op.setPromptTokens(r.usage!.prompt_tokens).setCompletionTokens(r.usage!.completion_tokens);
  },
);
```

## API surface

The SDK exposes the same surface as the .NET, Python, and Go SDKs:

- `init(options)` — boots the OpenTelemetry SDK (or attaches to an existing pipeline) and constructs the GenAI instruments. Idempotent.
- `startLlmCall(options)` — returns an `LlmCallScope` handle. Caller is responsible for `scope.end()`.
- `withLlmCall(options, fn)` — async-friendly equivalent. Auto-ends on success or rejection; records `error_kind` on rejection.
- `LlmCallScope` — handle methods: `setPromptTokens(n)`, `setCompletionTokens(n)`, `setResponseModel(model)`, `setErrorKind(kind)`, `setUsageDollars(amount)`, `end()`.
- `withTenantContext(ctx, parent?)` / `getTenantContext()` — propagate tenant fields via OTel baggage across async boundaries.

## What is emitted

On every `end()`:

- `gen_ai.client.operation.duration` (histogram, seconds) — request latency.
- `gen_ai.client.token.usage` (counter, `{token}`) — split by `gen_ai.token.type=input|output`.
- `llm_requests_total` (counter) — labelled by provider/model/route/tenant/team/app/env/project/error_kind.
- `llm_usage_dollars_total` (counter, USD) — runtime estimate when `setUsageDollars` is provided.

All counters/histograms carry the OTel GenAI keys (`gen_ai.system`, `gen_ai.request.model`, `gen_ai.response.model`, `gen_ai.operation.name`, `server.address`, `error.type`) and the project's `llm.*` extension keys for multi-tenant context.

## Examples

See [`examples/`](./examples/) for end-to-end wrappers around:

- `openai` (`examples/openai.ts`)
- `@anthropic-ai/sdk` (`examples/anthropic.ts`)
- `@google/generative-ai` (`examples/gemini.ts`)
- `@aws-sdk/client-bedrock-runtime` (`examples/bedrock.ts`)

## Run the tests

From the repo root (pnpm workspace):

```bash
pnpm install
pnpm --filter @openllm/metrics test
```

Or `pnpm test` (or `npm test`) from this directory. The `pretest` hook builds
`dist/` automatically, so no separate build step is needed.

## Compatibility

- Node.js ≥ 20.
- ESM + CJS dual export.
- Works alongside any host-managed OTel SDK (set `bootstrapOtel: false` in `init`).

## License

Apache-2.0. See [LICENSE](./LICENSE).
