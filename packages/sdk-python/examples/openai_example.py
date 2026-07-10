"""Minimal example: instrument an OpenAI chat completion with openllm-metrics.

Install once::

    pip install openllm-metrics openai

Run with valid ``OPENAI_API_KEY`` and an OTLP gRPC endpoint reachable at the
URL passed to ``openllm.init``. The SDK records token counts and latency on
exit of the ``with`` block — it never reads or transmits the prompt or
completion text.
"""

from __future__ import annotations

import os

import openai

import openllm


def main() -> None:
    openllm.init(
        service_name="openai-example",
        exporter_endpoint=os.environ.get(
            "OPENLLM_OTLP_ENDPOINT", "http://localhost:4317"
        ),
        default_tags={"deployment.environment": "demo"},
    )

    client = openai.OpenAI()
    model = "gpt-4o-mini"

    with openllm.llm_call(
        provider="openai",
        model=model,
        route="primary",
        tenant="acme",
        team="growth",
        app="chatbot",
        env="demo",
        project="customer-support",
    ) as op:
        try:
            response = client.chat.completions.create(
                model=model,
                messages=[
                    {"role": "user", "content": "Reply with a single word."}
                ],
            )
        except openai.OpenAIError as exc:
            op.set_error_kind(type(exc).__name__)
            raise

        usage = response.usage
        if usage is not None:
            op.set_prompt_tokens(usage.prompt_tokens)
            op.set_completion_tokens(usage.completion_tokens)

        # Optional: rough cost estimate from your pricing catalog. Omit if
        # cost mapping happens server-side (see F017).
        # op.set_usage_dollars(0.000_03 * usage.prompt_tokens
        #                    + 0.000_12 * usage.completion_tokens)


if __name__ == "__main__":
    main()
