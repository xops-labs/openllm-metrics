// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.
//
// Example: wrap the `openai` SDK with @openllm/metrics. Run with the
// project's existing OTel collector at `http://localhost:4318`.
//
// IMPORTANT: this example sends prompt text *to OpenAI*, but the SDK does
// not capture, log, or export the prompt or the completion. Only token
// counts, latency, and tenant labels flow through OTel.

import OpenAI from 'openai';
import { init, withLlmCall } from '@openllm/metrics';

async function main() {
  await init({
    serviceName: 'sdk-node-example-openai',
    exporterEndpoint: 'http://localhost:4318',
    defaultTags: { env: 'dev', project: 'openllm-demo' },
  });

  const client = new OpenAI();

  const result = await withLlmCall(
    {
      provider: 'openai',
      model: 'gpt-4o-mini',
      route: 'chat-primary',
      tenant: 'acme',
      team: 'platform-search',
      app: 'rag-svc',
      env: 'dev',
      project: 'acme-search',
      serverAddress: 'api.openai.com',
    },
    async (op) => {
      const response = await client.chat.completions.create({
        model: 'gpt-4o-mini',
        messages: [{ role: 'user', content: 'In one word: ping?' }],
      });
      if (response.usage) {
        op.setPromptTokens(response.usage.prompt_tokens);
        op.setCompletionTokens(response.usage.completion_tokens);
      }
      op.setResponseModel(response.model);
      return response;
    },
  );

  // Note: we *return* the response so the application can use it, but the
  // SDK never reaches into `result.choices[*].message.content`.
  return result;
}

main().catch((err) => {
  console.error('example failed:', err);
  process.exit(1);
});
