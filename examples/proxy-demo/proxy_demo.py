"""proxy_demo.py — Zero-code instrumentation via the OpenLLM Metrics gateway.

Run with::

    OPENAI_BASE_URL=http://localhost:8085/v1 python proxy_demo.py

The script makes a single OpenAI chat completion request.  The only change
from a vanilla OpenAI call is that ``base_url`` is read from the environment
variable set above — no SDK import, no ``openllm.init()``, no wrapper code.
The base URL must end in ``/v1`` because the OpenAI SDK appends
``/chat/completions`` to it.

The gateway intercepts the request, records timing and token counts, and
forwards it to ``api.openai.com``.  After the call you will see the
``llm_gateway_*`` series on the gateway metrics port (:8086/metrics) and the
aggregated ``llm_requests_total`` on the metrics-endpoint (:9092/metrics)
and in Prometheus.

Optional tenant headers let the gateway attribute the call without any
application-side changes:

    X-OLM-Tenant   acme
    X-OLM-Team     platform
    X-OLM-App      chatbot
    X-OLM-Env      production
    X-OLM-Project  customer-support
"""

from __future__ import annotations

import os
import sys

try:
    import openai
except ImportError:
    print("Install the openai package first:  pip install openai")
    sys.exit(1)

# Must end in /v1 — the OpenAI SDK appends /chat/completions to the base URL.
BASE_URL = os.environ.get("OPENAI_BASE_URL", "http://localhost:8085/v1")

client = openai.OpenAI(
    base_url=BASE_URL,
    # Optional: add tenant headers so the gateway labels the metrics.
    default_headers={
        "X-OLM-Tenant": "acme",
        "X-OLM-Team": "platform",
        "X-OLM-App": "proxy-demo",
        "X-OLM-Env": "development",
        "X-OLM-Project": "openllm-demo",
    },
)

response = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "Reply with exactly one word: pong"}],
)

print(f"Model: {response.model}")
print(f"Prompt tokens:     {response.usage.prompt_tokens}")
print(f"Completion tokens: {response.usage.completion_tokens}")
print(f"Gateway base URL:  {BASE_URL}")
print()
print("Done. Check http://localhost:8086/metrics for llm_gateway_requests_total,")
print("or http://localhost:9092/metrics / Prometheus for llm_requests_total.")
