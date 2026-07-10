# openllm — OpenLLM Metrics Go SDK

Idiomatic Go SDK for emitting OpenTelemetry GenAI signals (`gen_ai.*`) plus the
project-specific `llm_*` extension metrics from in-process LLM calls. Same
surface as the .NET, Python, and Node.js SDKs.

- OTel semconv: `gen_ai.client.operation.duration`,
  `gen_ai.client.token.usage` (split by `gen_ai.token.type=input|output`).
- Project extensions: `llm_requests_total`, `llm_usage_dollars`.
- Multi-tenant from day one — `tenant/team/app/env/project` are first-class.
- **Never collects prompt or completion text.** Token counts and usage
  metadata only.

## Install

> **Not yet `go get`-able.** The module path matches this repository
> (`github.com/yasvanth511/openllm-metrics-oss`), but the repo has not been
> published and tagged yet. Until then, depend on the SDK from source with a
> `replace` directive — this is how the bundled [examples](examples/) consume it:
>
> ```go
> require github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go v0.0.0
> replace github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go => ../path/to/packages/sdk-go
> ```

Once published, installation will be:

```sh
go get github.com/yasvanth511/openllm-metrics-oss/packages/sdk-go
```

Requires Go 1.25+.

## 10-line quickstart

```go
ctx := context.Background()
shutdown, _ := openllm.Init(ctx, openllm.Options{
    ServiceName:      "my-llm-app",
    ExporterEndpoint: "http://localhost:4318",
})
defer shutdown(ctx)

op, _ := openllm.StartLlmCall(ctx, openllm.CallOptions{
    Provider: "openai", Model: "gpt-4o-mini",
    Tenant: "acme", Team: "platform", App: "chatbot", Env: "production", Project: "alpha",
})
defer op.End()
// ... call your provider client, then:
op.SetPromptTokens(42); op.SetCompletionTokens(128)
```

## API

- `openllm.Init(ctx, openllm.Options{...}) (shutdown, err)` — boots OTel
  TracerProvider and MeterProvider over OTLP/HTTP and installs the
  TraceContext + Baggage propagators.
- `openllm.StartLlmCall(ctx, openllm.CallOptions{...}) (*LlmCall, context.Context)`
  — opens a `llm.call` span and attaches `tenant/team/app/env/project` to OTel
  baggage. **Use the returned context for downstream calls** so trace and
  tenant context propagate.
- `op.SetPromptTokens(n int64)`, `op.SetCompletionTokens(n int64)`,
  `op.SetErrorKind(kind string)`, `op.SetUsageDollars(amount float64)`.
- `op.End()` — emits the histogram, counters, and ends the span. Idempotent.

## Privacy

The SDK records token counts and dollarized usage. It does **not** read,
buffer, log, or transmit prompt or completion text. Anything you pass to your
provider client stays between you and the provider.

## Examples

- `examples/openai/` — wrap a community OpenAI Go client.
- `examples/anthropic/` — wrap a community Anthropic Go client.
- `examples/bedrock/` — wrap AWS SDK v2 `bedrockruntime`.

Each example is its own Go module so the main SDK module stays free of
provider dependencies. The examples are excluded from the repo-root `go.work`
for the same reason; a nested `examples/go.work` wires them to the SDK source,
so a plain build works from inside any example directory:

```sh
cd examples/openai
go build ./...   # or: go run .
```

## Run the tests

From this directory (`packages/sdk-go`):

```sh
go test ./...
```

CI runs the same suite with `-race`, which needs cgo — on a stock Windows
setup without a C toolchain, plain `go test ./...` is the supported fallback.

## License

Apache-2.0.
