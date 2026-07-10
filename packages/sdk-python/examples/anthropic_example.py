"""Minimal example: instrument an Anthropic Claude message with openllm-metrics.

Install once::

    pip install openllm-metrics anthropic

The SDK only reads token counts off the response object — it never inspects
the prompt or the model's reply text.
"""

from __future__ import annotations

import os

import anthropic

import openllm


def main() -> None:
    openllm.init(
        service_name="anthropic-example",
        exporter_endpoint=os.environ.get(
            "OPENLLM_OTLP_ENDPOINT", "http://localhost:4317"
        ),
        default_tags={"deployment.environment": "demo"},
    )

    client = anthropic.Anthropic()
    model = "claude-3-5-sonnet-latest"

    with openllm.llm_call(
        provider="anthropic",
        model=model,
        route="primary",
        tenant="acme",
        team="growth",
        app="chatbot",
        env="demo",
        project="customer-support",
    ) as op:
        try:
            message = client.messages.create(
                model=model,
                max_tokens=64,
                messages=[
                    {"role": "user", "content": "Reply with a single word."}
                ],
            )
        except anthropic.AnthropicError as exc:
            op.set_error_kind(type(exc).__name__)
            raise

        usage = message.usage
        op.set_prompt_tokens(usage.input_tokens)
        op.set_completion_tokens(usage.output_tokens)


if __name__ == "__main__":
    main()
