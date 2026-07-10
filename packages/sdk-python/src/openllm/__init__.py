"""OpenLLM Metrics runtime instrumentation SDK for Python.

Public surface — keep parity with the .NET, Node.js, and Go SDKs:

    import openllm

    openllm.init(
        service_name="my-app",
        exporter_endpoint="http://otel-collector:4317",
        default_tags={"deployment.environment": "prod"},
    )

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
        response = call_openai(...)
        op.set_prompt_tokens(response.usage.prompt_tokens)
        op.set_completion_tokens(response.usage.completion_tokens)

The SDK records token counts, latency, error category, and optional USD spend.
It NEVER captures prompt or completion text.
"""

from __future__ import annotations

from ._baggage import current_tenant_baggage
from ._call import LLMCall, llm_call
from ._init import init
from ._semconv import INSTRUMENTATION_VERSION as __version__

__all__ = ["LLMCall", "current_tenant_baggage", "init", "llm_call", "__version__"]
