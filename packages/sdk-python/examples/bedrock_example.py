"""Minimal example: instrument an Amazon Bedrock ``invoke_model`` call.

Install once::

    pip install openllm-metrics boto3

The example targets an Anthropic Claude model hosted on Bedrock. Bedrock
returns ``x-amzn-bedrock-input-token-count`` and
``x-amzn-bedrock-output-token-count`` headers; the parsed response body also
includes a ``usage`` block. Either path is sufficient; the SDK only reads the
counts, never the prompt or completion text.
"""

from __future__ import annotations

import json
import os

import boto3

import openllm


def main() -> None:
    openllm.init(
        service_name="bedrock-example",
        exporter_endpoint=os.environ.get(
            "OPENLLM_OTLP_ENDPOINT", "http://localhost:4317"
        ),
        default_tags={"deployment.environment": "demo"},
    )

    region = os.environ.get("AWS_REGION", "us-east-1")
    client = boto3.client("bedrock-runtime", region_name=region)
    model_id = "anthropic.claude-3-5-sonnet-20240620-v1:0"

    body = json.dumps(
        {
            "anthropic_version": "bedrock-2023-05-31",
            "max_tokens": 64,
            "messages": [
                {"role": "user", "content": "Reply with a single word."}
            ],
        }
    )

    with openllm.llm_call(
        provider="bedrock",
        model=model_id,
        route="primary",
        tenant="acme",
        team="growth",
        app="chatbot",
        env="demo",
        project="customer-support",
    ) as op:
        try:
            response = client.invoke_model(
                modelId=model_id,
                body=body,
                contentType="application/json",
                accept="application/json",
            )
        except Exception as exc:
            op.set_error_kind(type(exc).__name__)
            raise

        payload = json.loads(response["body"].read())
        usage = payload.get("usage", {})
        if "input_tokens" in usage:
            op.set_prompt_tokens(int(usage["input_tokens"]))
        if "output_tokens" in usage:
            op.set_completion_tokens(int(usage["output_tokens"]))


if __name__ == "__main__":
    main()
