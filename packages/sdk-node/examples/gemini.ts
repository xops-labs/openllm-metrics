// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.
//
// Example: wrap `@google/generative-ai` (Gemini) with @openllm/metrics.

import { GoogleGenerativeAI } from '@google/generative-ai';
import { init, withLlmCall } from '@openllm/metrics';

async function main() {
  await init({
    serviceName: 'sdk-node-example-gemini',
    exporterEndpoint: 'http://localhost:4318',
    defaultTags: { env: 'dev', project: 'openllm-demo' },
  });

  const genai = new GoogleGenerativeAI(process.env.GEMINI_API_KEY ?? '');
  const model = genai.getGenerativeModel({ model: 'gemini-1.5-flash' });

  return withLlmCall(
    {
      provider: 'gemini',
      model: 'gemini-1.5-flash',
      route: 'chat-primary',
      tenant: 'acme',
      team: 'platform-search',
      app: 'rag-svc',
      env: 'dev',
      project: 'acme-search',
      serverAddress: 'generativelanguage.googleapis.com',
    },
    async (op) => {
      const result = await model.generateContent('In one word: ack?');
      const usage = result.response.usageMetadata;
      if (usage) {
        op.setPromptTokens(usage.promptTokenCount ?? 0);
        op.setCompletionTokens(usage.candidatesTokenCount ?? 0);
      }
      return result;
    },
  );
}

main().catch((err) => {
  console.error('example failed:', err);
  process.exit(1);
});
