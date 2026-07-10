# OpenLLM.Metrics — .NET runtime instrumentation SDK

`OpenLLM.Metrics` is the .NET runtime SDK for [OpenLLM Metrics](https://github.com/yasvanth511/openllm-metrics-oss). It emits OpenTelemetry GenAI semantic-convention spans and metrics around every LLM call your .NET app makes, with multi-tenant labels (`tenant/team/app/env/project`) on every signal.

## Table of Contents

- [What it does](#what-it-does)
- [What it never does](#what-it-never-does)
- [Install](#install)
- [Quickstart: wrap an OpenAI call](#quickstart-wrap-an-openai-call)
- [Emitted signals](#emitted-signals)
- [Baggage propagation](#baggage-propagation)
- [Examples](#examples)
- [Run the tests](#run-the-tests)
- [Cross-language parity](#cross-language-parity)

## What it does

- Boots an OpenTelemetry `TracerProvider` and `MeterProvider` configured for GenAI semantic conventions.
- Exposes a `using var op = OpenLLM.StartLlmCall(...)` scope per LLM call.
- On `Dispose` records:
  - `gen_ai.client.operation.duration` (histogram, seconds).
  - `gen_ai.client.token.usage` (counter, split by `gen_ai.token.type=input|output`).
  - `llm_requests_total` (counter, labeled by `provider, model, route, tenant, team, app, env, project, error_kind`).
  - `llm_usage_dollars` (counter, only when `SetUsageDollars` is called).
- Sets the same labels on the underlying `Activity` (span name `chat <model>` per GenAI spec).
- Propagates `tenant/team/app/env/project` as W3C Baggage so downstream services pick them up automatically.

## What it never does

- It does **not** collect prompt or completion text. There is no `SetPrompt` or `SetCompletion` setter, and there will never be one. Token counts and call metadata only.
- It does not call provider APIs for you. You make the call; this SDK only times and labels it.

## Install

> **Not yet published to NuGet.** Until `OpenLLM.Metrics` is released, add a
> project reference to the SDK csproj (or build a local package with
> `dotnet pack`):
>
> ```sh
> dotnet add reference ../path/to/packages/sdk-dotnet/OpenLLM.Metrics/OpenLLM.Metrics.csproj
> ```

Once published, installation will be:

```sh
dotnet add package OpenLLM.Metrics --version 0.1.0
```

Targets `net8.0` and `net9.0`.

## Quickstart: wrap an OpenAI call

```csharp
using OpenLLMMetrics;

OpenLLM.Init("my-service", "http://localhost:4317");

using (var op = OpenLLM.StartLlmCall(
    provider: "openai", model: "gpt-4o-mini", route: "openai/us-east",
    tenant: "acme", team: "platform", app: "support-bot",
    env: "production", project: "demo"))
{
    var response = await openAiClient.GetChatClient("gpt-4o-mini")
        .CompleteChatAsync(messages);
    op.SetPromptTokens(response.Value.Usage.InputTokenCount);
    op.SetCompletionTokens(response.Value.Usage.OutputTokenCount);
}
```

The 10 lines above emit the duration histogram, two token counters, and the `llm_requests_total` counter, all labeled with the multi-tenant identity bundle.

## Emitted signals

| Instrument                         | Type      | Unit        | Labels                                                                       |
| ---------------------------------- | --------- | ----------- | ---------------------------------------------------------------------------- |
| `gen_ai.client.operation.duration` | histogram | `s`         | provider, model, route, tenant, team, app, env, project, error_kind          |
| `gen_ai.client.token.usage`        | counter   | `{token}`   | + `gen_ai.token.type=input` or `output`                                      |
| `llm_requests_total`               | counter   | `{request}` | provider, model, route, tenant, team, app, env, project, error_kind          |
| `llm_usage_dollars`                | counter   | `USD`       | provider, model, route, tenant, team, app, env, project, error_kind (opt-in) |

Plus an `Activity` named `chat <model>` carrying the same attributes for trace-to-metric correlation.

## Baggage propagation

`StartLlmCall` injects `tenant/team/app/env/project` onto the current `Activity` as W3C Baggage. Any HTTP / gRPC call you make inside the `using` block automatically carries these in the `baggage` header, so the gateway and downstream services see the same identity bundle without re-plumbing it.

## Examples

- [`examples/OpenAI.Example`](examples/OpenAI.Example/Program.cs)
- [`examples/AzureOpenAI.Example`](examples/AzureOpenAI.Example/Program.cs)

## Run the tests

From this directory (`packages/sdk-dotnet`):

```sh
dotnet test tests/OpenLLM.Metrics.Tests
```

This restores, builds, and runs the xUnit smoke suite. Requires a .NET SDK
new enough for the test project's `net10.0` target (the library itself
targets `net8.0`/`net9.0`).

## Cross-language parity

The same surface ships for Go, Python, and Node.js. Span names, attribute keys, and instrument names match across every language so a single Grafana dashboard works for a polyglot fleet.
