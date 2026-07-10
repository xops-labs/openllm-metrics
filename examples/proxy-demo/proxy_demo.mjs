/**
 * proxy_demo.mjs — Zero-code instrumentation via the OpenLLM Metrics gateway.
 *
 * Run with:
 *   OPENAI_BASE_URL=http://localhost:8085/v1 node proxy_demo.mjs
 *
 * The only change from a vanilla OpenAI call is `baseURL` coming from the
 * environment variable set above — no SDK import, no `init()`, no wrapper.
 * The base URL must end in /v1 because the OpenAI SDK appends
 * /chat/completions to it.
 *
 * The gateway intercepts the request, records timing and token counts, and
 * forwards it to api.openai.com.  After the call you will see the
 * llm_gateway_* series on the gateway metrics port (:8086/metrics) and the
 * aggregated llm_requests_total on the metrics-endpoint (:9092/metrics)
 * and in Prometheus.
 *
 * Optional tenant headers let the gateway attribute the call without any
 * application-side changes:
 *
 *   X-OLM-Tenant   acme
 *   X-OLM-Team     platform
 *   X-OLM-App      chatbot
 *   X-OLM-Env      production
 *   X-OLM-Project  customer-support
 */

import OpenAI from 'openai';

// Must end in /v1 — the OpenAI SDK appends /chat/completions to the base URL.
const BASE_URL = process.env.OPENAI_BASE_URL ?? 'http://localhost:8085/v1';

const client = new OpenAI({
  baseURL: BASE_URL,
  // Optional: add tenant headers so the gateway labels the metrics.
  defaultHeaders: {
    'X-OLM-Tenant': 'acme',
    'X-OLM-Team': 'platform',
    'X-OLM-App': 'proxy-demo',
    'X-OLM-Env': 'development',
    'X-OLM-Project': 'openllm-demo',
  },
});

const response = await client.chat.completions.create({
  model: 'gpt-4o-mini',
  messages: [{ role: 'user', content: 'Reply with exactly one word: pong' }],
});

console.log(`Model:             ${response.model}`);
console.log(`Prompt tokens:     ${response.usage?.prompt_tokens}`);
console.log(`Completion tokens: ${response.usage?.completion_tokens}`);
console.log(`Gateway base URL:  ${BASE_URL}`);
console.log('');
console.log('Done. Check http://localhost:8086/metrics for llm_gateway_requests_total,');
console.log('or http://localhost:9092/metrics / Prometheus for llm_requests_total.');
