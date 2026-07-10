# openllm-metrics (Python SDK)

Runtime instrumentation SDK for OpenLLM Metrics, the open-source telemetry
control plane for multi-provider LLM API operations.

- Records token counts, latency, error category, and optional USD spend.
- Aligns with [OpenTelemetry GenAI semantic conventions](https://opentelemetry.io/docs/specs/semconv/gen-ai/) for traces and metrics, plus `llm_*` extension metrics for tenant-scoped reliability and cost views.
- Multi-tenant from day one: every signal carries `tenant`, `team`, `app`, `env`, `project`.
- **Never collects prompt or completion text** — token counts and usage metadata only.

Parity contract with the .NET, Node.js, and Go SDKs: same `init(...)` boot, same `llm_call(...)` context manager, same metric names.

## Install

> **Not yet published to PyPI.** Until `openllm-metrics` is released, install
> from source:
>
> ```bash
> pip install ./packages/sdk-python
> # or, once the repo is public, directly from Git:
> # pip install "git+https://github.com/yasvanth511/openllm-metrics-oss.git#subdirectory=packages/sdk-python"
> ```

Once published, installation will be:

```bash
pip install openllm-metrics
```

Python 3.10+ required.

## Quickstart

```python
import openllm

openllm.init("my-app", "http://otel-collector:4317", {"deployment.environment": "prod"})

with openllm.llm_call(provider="openai", model="gpt-4o-mini", route="primary",
                      tenant="acme", team="growth", app="chatbot",
                      env="prod", project="customer-support") as op:
    response = client.chat.completions.create(model="gpt-4o-mini", messages=[...])
    op.set_prompt_tokens(response.usage.prompt_tokens)
    op.set_completion_tokens(response.usage.completion_tokens)
```

That is the full integration: one boot call, one context manager per request.

## Signals emitted

On exit of each `with openllm.llm_call(...)` block the SDK emits:

| Signal                             | Type                     | Description                                                                                        |
| ---------------------------------- | ------------------------ | -------------------------------------------------------------------------------------------------- |
| `gen_ai.client.operation.duration` | OTel histogram (seconds) | End-to-end latency of the LLM call.                                                                |
| `gen_ai.client.token.usage`        | OTel counter (`{token}`) | Token counts split by `gen_ai.token.type=input\|output`.                                           |
| `llm_requests_total`               | Counter                  | Labelled by `provider`, `model`, `route`, `tenant`, `team`, `app`, `env`, `project`, `error_kind`. |
| `llm_usage_dollars`                | Counter (USD)            | Emitted only when `op.set_usage_dollars(...)` is called.                                           |
| Span `chat <model>`                | OTel trace               | Closed with status `OK` or `ERROR` and `error.type`.                                               |

The tenant context (`tenant/team/app/env/project`) is also written to OTel
baggage so downstream pipelines see the same values.

## Handle API

```python
op.set_prompt_tokens(int)
op.set_completion_tokens(int)
op.set_error_kind(str | None)   # normalized category, e.g. "rate_limit"
op.set_usage_dollars(float | None)
```

## Provider examples

- [`examples/openai_example.py`](examples/openai_example.py)
- [`examples/anthropic_example.py`](examples/anthropic_example.py)
- [`examples/gemini_example.py`](examples/gemini_example.py)
- [`examples/bedrock_example.py`](examples/bedrock_example.py)

## Run the tests

From this directory (`packages/sdk-python`), install the SDK in editable mode
with the `dev` extra (pulls in pytest), then run the suite:

```bash
pip install -e ".[dev]"
python -m pytest
```

`pyproject.toml` already points pytest at `tests/`, so no extra arguments are
needed.

## Privacy

The SDK has no API for prompt, completion, system-message, tool-call, or
embedding payload capture. Token counts and usage metadata are the only
content-derived data points it ever records.

## License

Apache-2.0. See [LICENSE](LICENSE).
