"""Smoke tests for the openllm-metrics Python SDK.

All tests use in-memory OTel components — no live OTLP collector required.
The fixture wires up InMemorySpanExporter + InMemoryMetricReader and injects
a _Runtime instance directly so the real gRPC exporter is never instantiated.
"""

from __future__ import annotations

import sys
import os

# Ensure the SDK source is importable when running from the repo root.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "..", "src"))

import pytest

from opentelemetry import metrics as otel_metrics, trace as otel_trace
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import InMemoryMetricReader
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

import openllm
from openllm._init import (  # type: ignore[attr-defined]
    _Runtime,
    _reset_for_testing,
    INSTRUMENTATION_NAME,
    INSTRUMENTATION_VERSION,
    _lock,
)
import openllm._init as _init_module
from openllm._semconv import (  # type: ignore[attr-defined]
    METRIC_CLIENT_OPERATION_DURATION,
    METRIC_CLIENT_TOKEN_USAGE,
    METRIC_LLM_REQUESTS_TOTAL,
    METRIC_LLM_USAGE_DOLLARS,
)


# ---------------------------------------------------------------------------
# Fixture
# ---------------------------------------------------------------------------


@pytest.fixture()
def in_memory_sdk():
    """Yield (span_exporter, metric_reader, runtime) backed by in-memory stores."""
    span_exporter = InMemorySpanExporter()
    tracer_provider = TracerProvider()
    tracer_provider.add_span_processor(SimpleSpanProcessor(span_exporter))
    otel_trace.set_tracer_provider(tracer_provider)

    metric_reader = InMemoryMetricReader()
    meter_provider = MeterProvider(metric_readers=[metric_reader])
    otel_metrics.set_meter_provider(meter_provider)

    tracer = tracer_provider.get_tracer(INSTRUMENTATION_NAME, INSTRUMENTATION_VERSION)
    meter = meter_provider.get_meter(INSTRUMENTATION_NAME, INSTRUMENTATION_VERSION)

    operation_duration = meter.create_histogram(
        name=METRIC_CLIENT_OPERATION_DURATION, unit="s"
    )
    token_usage = meter.create_counter(
        name=METRIC_CLIENT_TOKEN_USAGE, unit="{token}"
    )
    requests_total = meter.create_counter(
        name=METRIC_LLM_REQUESTS_TOTAL, unit="{request}"
    )
    usage_dollars = meter.create_counter(
        name=METRIC_LLM_USAGE_DOLLARS, unit="USD"
    )

    runtime = _Runtime(
        service_name="smoke-test",
        exporter_endpoint="",
        default_tags={},
        tracer=tracer,
        meter=meter,
        operation_duration=operation_duration,
        token_usage=token_usage,
        requests_total=requests_total,
        usage_dollars=usage_dollars,
    )

    with _lock:
        _init_module._runtime = runtime

    yield span_exporter, metric_reader, runtime

    tracer_provider.force_flush()
    _reset_for_testing()


# ---------------------------------------------------------------------------
# Helper
# ---------------------------------------------------------------------------


def _metric_names(metric_reader: InMemoryMetricReader) -> set[str]:
    data = metric_reader.get_metrics_data()
    names: set[str] = set()
    if data is None:
        return names
    for rm in data.resource_metrics:
        for sm in rm.scope_metrics:
            for m in sm.metrics:
                names.add(m.name)
    return names


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------


def test_span_name_matches_operation_and_model(in_memory_sdk):
    """Span name is '<operation> <model>' per GenAI semconv."""
    span_exporter, _, _ = in_memory_sdk

    with openllm.llm_call(
        provider="openai",
        model="gpt-4o-mini",
        tenant="acme",
        team="platform",
        app="smoke",
        env="test",
        project="openllm-test",
    ) as op:
        op.set_prompt_tokens(10)
        op.set_completion_tokens(25)

    spans = span_exporter.get_finished_spans()
    assert len(spans) >= 1
    assert spans[-1].name == "chat gpt-4o-mini"


def test_span_carries_genai_attributes(in_memory_sdk):
    """Span attributes include gen_ai.system and gen_ai.request.model."""
    span_exporter, _, _ = in_memory_sdk

    with openllm.llm_call(
        provider="anthropic",
        model="claude-3-haiku",
        tenant="acme",
        team="research",
        app="lab",
        env="staging",
        project="alpha",
    ):
        pass

    spans = span_exporter.get_finished_spans()
    assert len(spans) >= 1
    attrs = dict(spans[-1].attributes or {})
    assert attrs.get("gen_ai.system") == "anthropic"
    assert attrs.get("gen_ai.request.model") == "claude-3-haiku"


def test_span_carries_tenant_attributes(in_memory_sdk):
    """Tenant bundle (tenant/team/app/env/project) appear on the span."""
    span_exporter, _, _ = in_memory_sdk

    with openllm.llm_call(
        provider="openai",
        model="gpt-4o",
        tenant="tenantA",
        team="teamB",
        app="appC",
        env="prod",
        project="projD",
    ):
        pass

    spans = span_exporter.get_finished_spans()
    assert len(spans) >= 1
    attrs = dict(spans[-1].attributes or {})
    assert attrs.get("tenant") == "tenantA"
    assert attrs.get("team") == "teamB"
    assert attrs.get("app") == "appC"
    assert attrs.get("env") == "prod"
    assert attrs.get("project") == "projD"


def test_metrics_emitted_on_exit(in_memory_sdk):
    """All four metric instruments are recorded on context-manager exit."""
    _, metric_reader, _ = in_memory_sdk

    with openllm.llm_call(
        provider="openai",
        model="gpt-4o-mini",
        tenant="acme",
        team="platform",
        app="metrics-test",
        env="test",
        project="openllm-test",
    ) as op:
        op.set_prompt_tokens(42)
        op.set_completion_tokens(128)
        op.set_usage_dollars(0.001)

    names = _metric_names(metric_reader)
    assert METRIC_CLIENT_OPERATION_DURATION in names, f"missing {METRIC_CLIENT_OPERATION_DURATION}; got {names}"
    assert METRIC_CLIENT_TOKEN_USAGE in names, f"missing {METRIC_CLIENT_TOKEN_USAGE}; got {names}"
    assert METRIC_LLM_REQUESTS_TOTAL in names, f"missing {METRIC_LLM_REQUESTS_TOTAL}; got {names}"
    assert METRIC_LLM_USAGE_DOLLARS in names, f"missing {METRIC_LLM_USAGE_DOLLARS}; got {names}"


def test_error_kind_sets_span_status_error(in_memory_sdk):
    """set_error_kind marks the span with ERROR status and error.type attribute."""
    span_exporter, _, _ = in_memory_sdk

    with openllm.llm_call(
        provider="openai",
        model="gpt-4o-mini",
        tenant="acme",
        team="platform",
        app="error-test",
        env="test",
        project="openllm-test",
    ) as op:
        op.set_error_kind("rate_limit")

    spans = span_exporter.get_finished_spans()
    assert len(spans) >= 1
    span = spans[-1]
    attrs = dict(span.attributes or {})
    assert attrs.get("error.type") == "rate_limit"

    from opentelemetry.trace import StatusCode
    assert span.status.status_code == StatusCode.ERROR


def test_exception_in_context_auto_sets_error_kind(in_memory_sdk):
    """An unhandled exception inside llm_call auto-populates error_kind."""
    span_exporter, _, _ = in_memory_sdk

    with pytest.raises(ValueError):
        with openllm.llm_call(
            provider="openai",
            model="gpt-4o-mini",
            tenant="acme",
            team="platform",
            app="exc-test",
            env="test",
            project="openllm-test",
        ):
            raise ValueError("simulated error")

    spans = span_exporter.get_finished_spans()
    assert len(spans) >= 1
    attrs = dict(spans[-1].attributes or {})
    assert attrs.get("error.type") == "ValueError"


def test_dispose_is_idempotent(in_memory_sdk):
    """Exiting the context manager twice does not double-emit a span."""
    span_exporter, _, _ = in_memory_sdk

    handle = openllm.llm_call(provider="openai", model="gpt-4o-mini")
    handle.__enter__()
    handle.__exit__(None, None, None)
    handle.__exit__(None, None, None)  # second exit must be a no-op

    spans = span_exporter.get_finished_spans()
    assert len(spans) == 1, f"expected 1 span, got {len(spans)}"


def test_init_not_called_raises_runtime_error():
    """_get_runtime raises RuntimeError when init() has not been called."""
    _reset_for_testing()
    from openllm._init import _get_runtime  # type: ignore[attr-defined]
    with pytest.raises(RuntimeError, match="openllm.init"):
        _get_runtime()
