// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import (
	"encoding/json"
)

// ParseBedrock extracts usage from an AWS Bedrock `Invoke` response.
//
// Bedrock wraps each underlying model's native response shape, so the
// parser tries the small set of well-known token-bearing fields that
// appear across Anthropic-on-Bedrock, Titan, Llama, Mistral, and Cohere
// model families. If none are present the parser returns ok=false — this
// is normal for image / embedding models that simply do not emit token
// counts.
//
// For Bedrock `:invoke-with-response-stream` (AWS event-stream framing)
// the gateway streaming layer is responsible for unwrapping each event
// payload before handing it here; this parser sees the inner JSON only.
func ParseBedrock(body []byte) (Tokens, bool) {
	if len(body) == 0 {
		return Tokens{}, false
	}
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return Tokens{}, false
	}
	var resp struct {
		// Anthropic-on-Bedrock and Cohere both use this shape.
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
		// Titan / Llama-on-Bedrock emit these top-level fields.
		InputTokenCount  int `json:"inputTokenCount"`
		OutputTokenCount int `json:"outputTokenCount"`
		PromptTokenCount int `json:"prompt_token_count"`
		GenerationCount  int `json:"generation_token_count"`
		// AWS Bedrock Converse / cross-model "amazon-bedrock-invocationMetrics".
		Metrics *struct {
			InputTokenCount  int `json:"inputTokenCount"`
			OutputTokenCount int `json:"outputTokenCount"`
		} `json:"amazon-bedrock-invocationMetrics"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Tokens{}, false
	}
	t := Tokens{}
	if resp.Usage != nil {
		t.Input = pickPositive(resp.Usage.InputTokens, resp.Usage.PromptTokens)
		t.Output = resp.Usage.OutputTokens
	}
	if t.Input == 0 {
		t.Input = pickPositive(resp.InputTokenCount, resp.PromptTokenCount)
	}
	if t.Output == 0 {
		t.Output = pickPositive(resp.OutputTokenCount, resp.GenerationCount)
	}
	if t.Input == 0 && resp.Metrics != nil {
		t.Input = resp.Metrics.InputTokenCount
	}
	if t.Output == 0 && resp.Metrics != nil {
		t.Output = resp.Metrics.OutputTokenCount
	}
	if t.Input == 0 && t.Output == 0 {
		return Tokens{}, false
	}
	return t.finalize(), true
}
