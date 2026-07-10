// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.
//
// Example: wrap `@anthropic-ai/sdk` with @openllm/metrics.

import Anthropic from '@anthropic-ai/sdk';
import { init, startLlmCall } from '@openllm/metrics';

async function main() {
  await init({
    serviceName: 'sdk-node-example-anthropic',
    exporterEndpoint: 'http://localhost:4318',
    defaultTags: { env: 'dev', project: 'openllm-demo' },
  });

  const client = new Anthropic();

  // This example uses the lower-level `startLlmCall` + manual `op.end()`
  // pattern. Both shapes are first-class — see the OpenAI example for
  // `withLlmCall`, which is preferred for new code.
  const op = startLlmCall({
    provider: 'anthropic',
    model: 'claude-3-5-sonnet-latest',
    route: 'chat-primary',
    tenant: 'acme',
    team: 'platform-search',
    app: 'rag-svc',
    env: 'dev',
    project: 'acme-search',
    serverAddress: 'api.anthropic.com',
  });

  try {
    const response = await client.messages.create({
      model: 'claude-3-5-sonnet-latest',
      max_tokens: 64,
      messages: [{ role: 'user', content: 'In one word: pong?' }],
    });
    op.setPromptTokens(response.usage.input_tokens);
    op.setCompletionTokens(response.usage.output_tokens);
    op.setResponseModel(response.model);
  } catch (err) {
    op.setErrorKind(err instanceof Error ? err.name : 'error');
    throw err;
  } finally {
    op.end();
  }
}

main().catch((err) => {
  console.error('example failed:', err);
  process.exit(1);
});
