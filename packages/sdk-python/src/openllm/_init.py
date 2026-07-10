"""Boot helper that wires up the OpenTelemetry SDK for OpenLLM Metrics.

``init`` is idempotent: calling it twice with the same arguments returns the
existing tracer/meter wiring. It installs an OTLP gRPC exporter for both
traces and metrics, registers a resource that carries ``service.name`` plus
the caller's default tags, and stores a process-wide reference to the meter
plus the two GenAI instruments the ``llm_call`` context manager records into.
"""

from __future__ import annotations

import threading
from dataclasses import dataclass, field
from typing import Mapping

from opentelemetry import metrics, trace
from opentelemetry.exporter.otlp.proto.grpc.metric_exporter import (
    OTLPMetricExporter,
)
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.metrics import MeterProvider
from opentelemetry.sdk.metrics.export import PeriodicExportingMetricReader
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor

from ._semconv import (
    INSTRUMENTATION_NAME,
    INSTRUMENTATION_VERSION,
    METRIC_CLIENT_OPERATION_DURATION,
    METRIC_CLIENT_TOKEN_USAGE,
    METRIC_LLM_REQUESTS_TOTAL,
    METRIC_LLM_USAGE_DOLLARS,
)


@dataclass(frozen=True)
class _Runtime:
    """Process-wide handle to the boot-time tracer, meter, and instruments."""

    service_name: str
    exporter_endpoint: str
    tracer: trace.Tracer = field(repr=False)
    meter: metrics.Meter = field(repr=False)
    operation_duration: metrics.Histogram = field(repr=False)
    token_usage: metrics.Counter = field(repr=False)
    requests_total: metrics.Counter = field(repr=False)
    usage_dollars: metrics.Counter = field(repr=False)
    default_tags: Mapping[str, str] = field(default_factory=dict)


_lock = threading.Lock()
_runtime: _Runtime | None = None


def init(
    service_name: str,
    exporter_endpoint: str,
    default_tags: Mapping[str, str] | None = None,
) -> None:
    """Boot the OpenTelemetry SDK for OpenLLM Metrics.

    Parameters
    ----------
    service_name:
        Value for the OTel ``service.name`` resource attribute. Should match
        the ``app`` dimension used by the gateway and dashboards.
    exporter_endpoint:
        OTLP gRPC endpoint URL (for example ``http://otel-collector:4317``).
        Sent for both traces and metrics.
    default_tags:
        Optional resource attributes merged onto every signal — typically
        ``deployment.environment``, ``service.version``, ``service.namespace``.

    Calling ``init`` twice is safe; the second call is a no-op and the
    original wiring is reused. Tests that need a fresh runtime should use
    :func:`_reset_for_testing`.
    """
    global _runtime
    with _lock:
        if _runtime is not None:
            return

        attributes: dict[str, str] = {"service.name": service_name}
        if default_tags:
            for key, value in default_tags.items():
                if value is None:
                    continue
                attributes[str(key)] = str(value)

        resource = Resource.create(attributes)

        tracer_provider = TracerProvider(resource=resource)
        tracer_provider.add_span_processor(
            BatchSpanProcessor(OTLPSpanExporter(endpoint=exporter_endpoint))
        )
        trace.set_tracer_provider(tracer_provider)

        metric_reader = PeriodicExportingMetricReader(
            OTLPMetricExporter(endpoint=exporter_endpoint)
        )
        meter_provider = MeterProvider(
            resource=resource, metric_readers=[metric_reader]
        )
        metrics.set_meter_provider(meter_provider)

        tracer = trace.get_tracer(INSTRUMENTATION_NAME, INSTRUMENTATION_VERSION)
        meter = metrics.get_meter(INSTRUMENTATION_NAME, INSTRUMENTATION_VERSION)

        operation_duration = meter.create_histogram(
            name=METRIC_CLIENT_OPERATION_DURATION,
            description="LLM client operation duration",
            unit="s",
        )
        token_usage = meter.create_counter(
            name=METRIC_CLIENT_TOKEN_USAGE,
            description="LLM token usage per request, split by gen_ai.token.type",
            unit="{token}",
        )
        requests_total = meter.create_counter(
            name=METRIC_LLM_REQUESTS_TOTAL,
            description=(
                "Count of LLM requests labelled by provider, model, route, "
                "tenant, team, app, env, project, and error_kind"
            ),
            unit="{request}",
        )
        usage_dollars = meter.create_counter(
            name=METRIC_LLM_USAGE_DOLLARS,
            description="Estimated USD spend per LLM request",
            unit="USD",
        )

        _runtime = _Runtime(
            service_name=service_name,
            exporter_endpoint=exporter_endpoint,
            default_tags=dict(default_tags or {}),
            tracer=tracer,
            meter=meter,
            operation_duration=operation_duration,
            token_usage=token_usage,
            requests_total=requests_total,
            usage_dollars=usage_dollars,
        )


def _get_runtime() -> _Runtime:
    """Return the active runtime, raising if :func:`init` has not been called."""
    runtime = _runtime
    if runtime is None:
        raise RuntimeError(
            "openllm.init(...) must be called before openllm.llm_call(...). "
            "Call init once at process startup with your service name and "
            "OTLP endpoint."
        )
    return runtime


def _reset_for_testing() -> None:
    """Drop the cached runtime so the next ``init`` call rewires the SDK.

    Intended for unit tests only. Application code should treat ``init`` as a
    once-per-process operation.
    """
    global _runtime
    with _lock:
        _runtime = None
