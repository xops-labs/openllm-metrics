// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import "testing"

// fixtures here are response *shapes* only — the model label and structural
// fields. Consistent with the package privacy invariant, no prompt or
// completion text appears in any fixture.

func TestParseModel(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		body     string
		want     string
	}{
		{
			name:     "openai non-streaming top-level model",
			provider: providerOpenAI,
			body:     `{"id":"chatcmpl-abc","object":"chat.completion","model":"gpt-4o-mini","usage":{"prompt_tokens":42,"completion_tokens":128,"total_tokens":170}}`,
			want:     "gpt-4o-mini",
		},
		{
			name:     "openai streaming chunk carries model with data prefix",
			provider: providerOpenAI,
			body:     `data: {"object":"chat.completion.chunk","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			want:     "gpt-4o",
		},
		{
			name:     "anthropic non-streaming top-level model",
			provider: providerAnthropic,
			body:     `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4","usage":{"input_tokens":58,"output_tokens":204}}`,
			want:     "claude-sonnet-4",
		},
		{
			name:     "anthropic message_start carries message.model",
			provider: providerAnthropic,
			body:     `data: {"type":"message_start","message":{"id":"msg_1","model":"claude-opus-4","usage":{"input_tokens":58,"output_tokens":1}}}`,
			want:     "claude-opus-4",
		},
		{
			name:     "anthropic message_delta usage chunk has no model",
			provider: providerAnthropic,
			body:     `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":204}}`,
			want:     "",
		},
		{
			name:     "azure openai-shaped top-level model",
			provider: providerAzureOpenAI,
			body:     `{"model":"gpt-4o","usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}`,
			want:     "gpt-4o",
		},
		{
			name:     "openai body without model returns empty",
			provider: providerOpenAI,
			body:     `{"object":"chat.completion","usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`,
			want:     "",
		},
		{
			name:     "gemini returns empty (model comes from path)",
			provider: providerGemini,
			body:     `{"modelVersion":"gemini-1.5-pro","usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":85,"totalTokenCount":100}}`,
			want:     "",
		},
		{
			name:     "bedrock returns empty (model comes from path)",
			provider: providerBedrock,
			body:     `{"type":"message","model":"claude-on-bedrock","usage":{"input_tokens":33,"output_tokens":77}}`,
			want:     "",
		},
		{
			name:     "unknown provider returns empty",
			provider: "mystery",
			body:     `{"model":"x"}`,
			want:     "",
		},
		{
			name:     "empty body returns empty",
			provider: providerOpenAI,
			body:     ``,
			want:     "",
		},
		{
			name:     "done sentinel returns empty",
			provider: providerOpenAI,
			body:     `data: [DONE]`,
			want:     "",
		},
		{
			name:     "malformed json returns empty",
			provider: providerAnthropic,
			body:     `{"model":"claude"`,
			want:     "",
		},
		{
			name:     "non-json leading bytes return empty",
			provider: providerOpenAI,
			body:     `<html>502</html>`,
			want:     "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseModel(tc.provider, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("ParseModel(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

// TestParseModelAzureMatchesOpenAI locks in that the Azure surface reuses the
// OpenAI model implementation, mirroring the token-parser contract.
func TestParseModelAzureMatchesOpenAI(t *testing.T) {
	body := []byte(`{"model":"gpt-4o-mini","usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	if az, oa := ParseModel(providerAzureOpenAI, body), ParseModel(providerOpenAI, body); az != oa {
		t.Fatalf("azure model %q != openai model %q", az, oa)
	}
}
