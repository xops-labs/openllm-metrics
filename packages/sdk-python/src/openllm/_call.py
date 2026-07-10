"""``llm_call`` context manager: the per-request handle every Python integration
uses to record a single LLM operation.

Parity contract — identical surface to the .NET, Node.js, and Go SDKs:

* ``with openllm.llm_call(provider, model, route, tenant, team, app, env, project) as op:``
* ``op.set_prompt_tokens(int)`` / ``op.set_completion_tokens(int)``
* ``op.set_error_kind(str | None)``
* ``op.set_usage_dollars(float | None)``

On exit the context manager:

1. Emits the OTel histogram ``gen_ai.client.operation.duration``.
2. Emits the OTel counter ``gen_ai.client.token.usage`` once per token type
   (``input`` / ``output``).
3. Emits the ``llm_requests_total`` counter labelled by provider, model,
   route, tenant, team, app, env, project, and error_kind.
4. Emits ``llm_usage_dollars`` if the caller supplied a USD amount.
5. Closes the span ``chat <model>`` (or ``<operation> <model>``).
6. Detaches tenant baggage attached on entry.

The handle never accepts prompt or completion text; only counts and metadata.
"""

from __future__ import annotations

import time
from types import TracebackType
from typing import Any

from opentelemetry.trace import Span, SpanKind, Status, StatusCode

from ._baggage import attach_tenant_baggage, detach_tenant_baggage
from ._init import _get_runtime
from ._semconv import (
    DEFAULT_OPERATION,
    GEN_AI_ERROR_TYPE,
    GEN_AI_OPERATION_NAME,
    GEN_AI_REQUEST_MODEL,
    GEN_AI_SYSTEM,
    GEN_AI_TOKEN_TYPE,
    LLM_APP,
    LLM_ENV,
    LLM_ERROR_KIND,
    LLM_MODEL,
    LLM_PROJECT,
    LLM_PROVIDER,
    LLM_ROUTE,
    LLM_TEAM,
    LLM_TENANT,
    TOKEN_TYPE_INPUT,
    TOKEN_TYPE_OUTPUT,
)


class LLMCall:
    """Active LLM operation. Returned by :func:`llm_call` as the ``with`` target.

    Mutate the handle while inside the ``with`` block; the SDK reads the final
    state on ``__exit__`` and emits metrics + closes the span. Setters are
    idempotent — the last value supplied wins.
    """

    __slots__ = (
        "_provider",
        "_model",
        "_route",
        "_tenant",
        "_team",
        "_app",
        "_env",
        "_project",
        "_operation",
        "_prompt_tokens",
        "_completion_tokens",
        "_error_kind",
        "_usage_dollars",
        "_span",
        "_baggage_token",
        "_start_ns",
        "_closed",
    )

    def __init__(
        self,
        *,
        provider: str,
        model: str,
        route: str = "",
        tenant: str = "",
        team: str = "",
        app: str = "",
        env: str = "",
        project: str = "",
        operation: str = DEFAULT_OPERATION,
    ) -> None:
        self._provider = provider
        self._model = model
        self._route = route
        self._tenant = tenant
        self._team = team
        self._app = app
        self._env = env
        self._project = project
        self._operation = operation or DEFAULT_OPERATION
        self._prompt_tokens: int | None = None
        self._completion_tokens: int | None = None
        self._error_kind: str | None = None
        self._usage_dollars: float | None = None
        self._span: Span | None = None
        self._baggage_token: object | None = None
        self._start_ns: int = 0
        self._closed = False

    # ------------------------------------------------------------------
    # Setters mutated by the caller inside the ``with`` block.
    # ------------------------------------------------------------------

    def set_prompt_tokens(self, count: int) -> None:
        """Record the number of input/prompt tokens for this call."""
        if count is None:
            return
        self._prompt_tokens = max(int(count), 0)

    def set_completion_tokens(self, count: int) -> None:
        """Record the number of output/completion tokens for this call."""
        if count is None:
            return
        self._completion_tokens = max(int(count), 0)

    def set_error_kind(self, kind: str | None) -> None:
        """Set the normalized error category, or ``None`` to clear it."""
        if kind is None or kind == "":
            self._error_kind = None
        else:
            self._error_kind = str(kind)

    def set_usage_dollars(self, dollars: float | None) -> None:
        """Record an estimated USD cost for the call, or ``None`` to skip."""
        if dollars is None:
            self._usage_dollars = None
        else:
            self._usage_dollars = max(float(dollars), 0.0)

    # ------------------------------------------------------------------
    # Context manager protocol.
    # ------------------------------------------------------------------

    def __enter__(self) -> "LLMCall":
        runtime = _get_runtime()
        self._baggage_token = attach_tenant_baggage(
            tenant=self._tenant or None,
            team=self._team or None,
            app=self._app or None,
            env=self._env or None,
            project=self._project or None,
        )

        span_name = f"{self._operation} {self._model}".strip()
        span_attributes: dict[str, Any] = {
            GEN_AI_SYSTEM: self._provider,
            GEN_AI_REQUEST_MODEL: self._model,
            GEN_AI_OPERATION_NAME: self._operation,
        }
        if self._tenant:
            span_attributes[LLM_TENANT] = self._tenant
        if self._team:
            span_attributes[LLM_TEAM] = self._team
        if self._app:
            span_attributes[LLM_APP] = self._app
        if self._env:
            span_attributes[LLM_ENV] = self._env
        if self._project:
            span_attributes[LLM_PROJECT] = self._project
        if self._route:
            span_attributes[LLM_ROUTE] = self._route

        self._span = runtime.tracer.start_span(
            name=span_name,
            kind=SpanKind.CLIENT,
            attributes=span_attributes,
        )
        self._start_ns = time.perf_counter_ns()
        return self

    def __exit__(
        self,
        exc_type: type[BaseException] | None,
        exc: BaseException | None,
        tb: TracebackType | None,
    ) -> None:
        if self._closed:
            return
        self._closed = True

        elapsed_ns = time.perf_counter_ns() - self._start_ns
        duration_seconds = elapsed_ns / 1_000_000_000

        # If the caller did not set an error kind but an exception is in
        # flight, derive a default kind from the exception class name.
        if exc is not None and self._error_kind is None:
            self._error_kind = type(exc).__name__

        try:
            self._record_metrics(duration_seconds)
        finally:
            span = self._span
            if span is not None:
                if self._prompt_tokens is not None:
                    span.set_attribute(
                        "gen_ai.usage.input_tokens", int(self._prompt_tokens)
                    )
                if self._completion_tokens is not None:
                    span.set_attribute(
                        "gen_ai.usage.output_tokens", int(self._completion_tokens)
                    )
                if self._error_kind is not None:
                    span.set_attribute(GEN_AI_ERROR_TYPE, self._error_kind)
                    span.set_status(Status(StatusCode.ERROR, self._error_kind))
                    if exc is not None:
                        span.record_exception(exc)
                else:
                    span.set_status(Status(StatusCode.OK))
                span.end()

            token = self._baggage_token
            self._baggage_token = None
            if token is not None:
                detach_tenant_baggage(token)

    # ------------------------------------------------------------------
    # Internal: metric emission.
    # ------------------------------------------------------------------

    def _genai_attributes(self) -> dict[str, str]:
        attrs: dict[str, str] = {
            GEN_AI_SYSTEM: self._provider,
            GEN_AI_REQUEST_MODEL: self._model,
            GEN_AI_OPERATION_NAME: self._operation,
        }
        if self._error_kind is not None:
            attrs[GEN_AI_ERROR_TYPE] = self._error_kind
        return attrs

    def _llm_attributes(self) -> dict[str, str]:
        return {
            LLM_PROVIDER: self._provider,
            LLM_MODEL: self._model,
            LLM_ROUTE: self._route,
            LLM_TENANT: self._tenant,
            LLM_TEAM: self._team,
            LLM_APP: self._app,
            LLM_ENV: self._env,
            LLM_PROJECT: self._project,
            LLM_ERROR_KIND: self._error_kind or "",
        }

    def _record_metrics(self, duration_seconds: float) -> None:
        runtime = _get_runtime()

        genai_attrs = self._genai_attributes()
        runtime.operation_duration.record(duration_seconds, attributes=genai_attrs)

        if self._prompt_tokens:
            runtime.token_usage.add(
                int(self._prompt_tokens),
                attributes={**genai_attrs, GEN_AI_TOKEN_TYPE: TOKEN_TYPE_INPUT},
            )
        if self._completion_tokens:
            runtime.token_usage.add(
                int(self._completion_tokens),
                attributes={**genai_attrs, GEN_AI_TOKEN_TYPE: TOKEN_TYPE_OUTPUT},
            )

        llm_attrs = self._llm_attributes()
        runtime.requests_total.add(1, attributes=llm_attrs)
        if self._usage_dollars is not None and self._usage_dollars > 0:
            runtime.usage_dollars.add(
                float(self._usage_dollars), attributes=llm_attrs
            )


def llm_call(
    provider: str,
    model: str,
    route: str = "",
    tenant: str = "",
    team: str = "",
    app: str = "",
    env: str = "",
    project: str = "",
    operation: str = DEFAULT_OPERATION,
) -> LLMCall:
    """Open a new :class:`LLMCall` handle to instrument one LLM operation.

    Use as a context manager::

        with openllm.llm_call(
            provider="openai",
            model="gpt-4o-mini",
            route="primary",
            tenant="acme",
            team="growth",
            app="chatbot",
            env="prod",
            project="customer-support",
        ) as op:
            response = openai_client.chat.completions.create(...)
            op.set_prompt_tokens(response.usage.prompt_tokens)
            op.set_completion_tokens(response.usage.completion_tokens)

    The handle never accepts prompt or completion text.
    """
    if not provider:
        raise ValueError("openllm.llm_call: provider must be a non-empty string")
    if not model:
        raise ValueError("openllm.llm_call: model must be a non-empty string")
    return LLMCall(
        provider=provider,
        model=model,
        route=route,
        tenant=tenant,
        team=team,
        app=app,
        env=env,
        project=project,
        operation=operation,
    )


__all__ = ["LLMCall", "llm_call"]
