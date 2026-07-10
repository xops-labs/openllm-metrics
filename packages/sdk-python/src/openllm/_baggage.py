"""OpenTelemetry baggage helpers for multi-tenant context propagation.

Every LLM call in OpenLLM Metrics carries ``tenant``, ``team``, ``app``, ``env``,
and ``project`` so downstream pipelines (gateway, scoring worker, policy
evaluator, audit log) can attribute usage without re-deriving it. Baggage is
the OTel-native way to thread that context through async boundaries and across
process hops.
"""

from __future__ import annotations

from typing import Iterable

from opentelemetry import baggage, context as otel_context
from opentelemetry.context import Context

from ._semconv import (
    BAGGAGE_APP,
    BAGGAGE_ENV,
    BAGGAGE_PROJECT,
    BAGGAGE_TEAM,
    BAGGAGE_TENANT,
)

_TENANT_KEYS: tuple[tuple[str, str], ...] = (
    ("tenant", BAGGAGE_TENANT),
    ("team", BAGGAGE_TEAM),
    ("app", BAGGAGE_APP),
    ("env", BAGGAGE_ENV),
    ("project", BAGGAGE_PROJECT),
)


def attach_tenant_baggage(
    *,
    tenant: str | None,
    team: str | None,
    app: str | None,
    env: str | None,
    project: str | None,
    parent: Context | None = None,
) -> object:
    """Attach tenant context to OTel baggage and return a detach token.

    Empty or ``None`` values are skipped so a missing dimension does not
    overwrite a value already set higher in the call stack.

    The returned token MUST be passed to :func:`detach_tenant_baggage` to
    avoid leaking the baggage into unrelated work.
    """
    ctx = parent if parent is not None else otel_context.get_current()
    values: Iterable[tuple[str, str | None]] = (
        (BAGGAGE_TENANT, tenant),
        (BAGGAGE_TEAM, team),
        (BAGGAGE_APP, app),
        (BAGGAGE_ENV, env),
        (BAGGAGE_PROJECT, project),
    )
    for key, value in values:
        if value:
            ctx = baggage.set_baggage(key, value, context=ctx)
    return otel_context.attach(ctx)


def detach_tenant_baggage(token: object) -> None:
    """Detach a baggage scope previously attached by :func:`attach_tenant_baggage`."""
    otel_context.detach(token)  # type: ignore[arg-type]


def current_tenant_baggage() -> dict[str, str]:
    """Return the tenant-context keys currently set on OTel baggage.

    Keys are the short-form names (``tenant``, ``team``, ``app``, ``env``,
    ``project``); values are the strings stored on baggage. Missing keys are
    omitted from the returned dict.
    """
    result: dict[str, str] = {}
    for short, full in _TENANT_KEYS:
        value = baggage.get_baggage(full)
        if isinstance(value, str) and value:
            result[short] = value
    return result
