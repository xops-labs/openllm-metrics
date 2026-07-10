// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.
//
// Example: wrap `@aws-sdk/client-bedrock-runtime` with @openllm/metrics.

import { BedrockRuntimeClient, ConverseCommand } from '@aws-sdk/client-bedrock-runtime';
import { init, withLlmCall } from '@openllm/metrics';

async function main() {
  await init({
    serviceName: 'sdk-node-example-bedrock',
    exporterEndpoint: 'http://localhost:4318',
    defaultTags: { env: 'dev', project: 'openllm-demo' },
  });

  const region = process.env.AWS_REGION ?? 'us-east-1';
  const client = new BedrockRuntimeClient({ region });
  const modelId = 'anthropic.claude-3-5-sonnet-20240620-v1:0';

  return withLlmCall(
    {
      provider: 'bedrock',
      model: modelId,
      route: 'chat-primary',
      tenant: 'acme',
      team: 'platform-search',
      app: 'rag-svc',
      env: 'dev',
      project: 'acme-search',
      serverAddress: `bedrock-runtime.${region}.amazonaws.com`,
    },
    async (op) => {
      const response = await client.send(
        new ConverseCommand({
          modelId,
          messages: [{ role: 'user', content: [{ text: 'In one word: roger?' }] }],
        }),
      );
      if (response.usage) {
        op.setPromptTokens(response.usage.inputTokens ?? 0);
        op.setCompletionTokens(response.usage.outputTokens ?? 0);
      }
      return response;
    },
  );
}

main().catch((err) => {
  console.error('example failed:', err);
  process.exit(1);
});
