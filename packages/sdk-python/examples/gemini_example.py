"""Minimal example: instrument a Google Gemini call with openllm-metrics.

Install once::

    pip install openllm-metrics google-generativeai

The SDK reads ``usage_metadata`` for token counts; it never inspects the
prompt or response text.
"""

from __future__ import annotations

import os

import google.generativeai as genai

import openllm


def main() -> None:
    openllm.init(
        service_name="gemini-example",
        exporter_endpoint=os.environ.get(
            "OPENLLM_OTLP_ENDPOINT", "http://localhost:4317"
        ),
        default_tags={"deployment.environment": "demo"},
    )

    genai.configure(api_key=os.environ["GOOGLE_API_KEY"])

    model_name = "gemini-1.5-flash"
    model = genai.GenerativeModel(model_name)

    with openllm.llm_call(
        provider="gemini",
        model=model_name,
        route="primary",
        tenant="acme",
        team="growth",
        app="chatbot",
        env="demo",
        project="customer-support",
    ) as op:
        try:
            response = model.generate_content("Reply with a single word.")
        except Exception as exc:  # google-generativeai raises a varied set
            op.set_error_kind(type(exc).__name__)
            raise

        usage = getattr(response, "usage_metadata", None)
        if usage is not None:
            op.set_prompt_tokens(usage.prompt_token_count)
            op.set_completion_tokens(usage.candidates_token_count)


if __name__ == "__main__":
    main()
