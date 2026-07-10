// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import "testing"

// fixtures here are response *shapes* only — token integers and structural
// fields. Consistent with the package privacy invariant, no prompt or
// completion text appears in any fixture.

type parseCase struct {
	name   string
	body   string
	wantOK bool
	want   Tokens
}

func runParseCases(t *testing.T, parse func([]byte) (Tokens, bool), cases []parseCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parse([]byte(tc.body))
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (tokens=%+v)", ok, tc.wantOK, got)
			}
			if !tc.wantOK {
				if got != (Tokens{}) {
					t.Fatalf("on ok=false expected zero Tokens, got %+v", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("tokens = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseOpenAI(t *testing.T) {
	runParseCases(t, ParseOpenAI, []parseCase{
		{
			name:   "chat completion non-streaming",
			body:   `{"id":"chatcmpl-abc","object":"chat.completion","model":"gpt-4o-mini","choices":[{"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":42,"completion_tokens":128,"total_tokens":170}}`,
			wantOK: true,
			want:   Tokens{Input: 42, Output: 128, Total: 170},
		},
		{
			name:   "streaming final chunk with include_usage and data prefix",
			body:   `data: {"object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
			wantOK: true,
			want:   Tokens{Input: 10, Output: 5, Total: 15},
		},
		{
			name:   "responses API input_tokens/output_tokens, total computed",
			body:   `{"object":"response","usage":{"input_tokens":20,"output_tokens":30}}`,
			wantOK: true,
			want:   Tokens{Input: 20, Output: 30, Total: 50},
		},
		{
			name:   "usage absent",
			body:   `{"id":"x","object":"chat.completion","choices":[]}`,
			wantOK: false,
		},
		{
			name:   "usage present but all zero",
			body:   `{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`,
			wantOK: false,
		},
		{
			name:   "stream DONE sentinel",
			body:   `data: [DONE]`,
			wantOK: false,
		},
	})
}

func TestParseAnthropic(t *testing.T) {
	runParseCases(t, ParseAnthropic, []parseCase{
		{
			name:   "messages non-streaming",
			body:   `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4","usage":{"input_tokens":58,"output_tokens":204}}`,
			wantOK: true,
			want:   Tokens{Input: 58, Output: 204, Total: 262},
		},
		{
			name:   "message_start SSE carries input plus seed output",
			body:   `data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":58,"output_tokens":1}}}`,
			wantOK: true,
			want:   Tokens{Input: 58, Output: 1, Total: 59},
		},
		{
			name:   "message_delta SSE carries output only",
			body:   `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":204}}`,
			wantOK: true,
			want:   Tokens{Input: 0, Output: 204, Total: 204},
		},
		{
			name:   "content_block_delta has no usage",
			body:   `data: {"type":"content_block_delta","delta":{"type":"text_delta"}}`,
			wantOK: false,
		},
	})
}

func TestParseGemini(t *testing.T) {
	runParseCases(t, ParseGemini, []parseCase{
		{
			name:   "generateContent with usageMetadata",
			body:   `{"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":85,"totalTokenCount":100}}`,
			wantOK: true,
			want:   Tokens{Input: 15, Output: 85, Total: 100},
		},
		{
			name:   "streamGenerateContent SSE final chunk",
			body:   `data: {"candidates":[{"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":15,"candidatesTokenCount":85,"totalTokenCount":100}}`,
			wantOK: true,
			want:   Tokens{Input: 15, Output: 85, Total: 100},
		},
		{
			name:   "total computed when totalTokenCount absent",
			body:   `{"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":8}}`,
			wantOK: true,
			want:   Tokens{Input: 12, Output: 8, Total: 20},
		},
		{
			name:   "partial streaming chunk without usageMetadata",
			body:   `{"candidates":[{"content":{"parts":[{}]}}]}`,
			wantOK: false,
		},
	})
}

func TestParseBedrock(t *testing.T) {
	runParseCases(t, ParseBedrock, []parseCase{
		{
			name:   "anthropic-on-bedrock usage object",
			body:   `{"type":"message","usage":{"input_tokens":33,"output_tokens":77}}`,
			wantOK: true,
			want:   Tokens{Input: 33, Output: 77, Total: 110},
		},
		{
			name:   "titan-style top-level camelCase counts",
			body:   `{"inputTokenCount":12,"outputTokenCount":40}`,
			wantOK: true,
			want:   Tokens{Input: 12, Output: 40, Total: 52},
		},
		{
			name:   "llama-style snake_case counts",
			body:   `{"prompt_token_count":18,"generation_token_count":52}`,
			wantOK: true,
			want:   Tokens{Input: 18, Output: 52, Total: 70},
		},
		{
			name:   "invocation metrics block",
			body:   `{"amazon-bedrock-invocationMetrics":{"inputTokenCount":7,"outputTokenCount":9}}`,
			wantOK: true,
			want:   Tokens{Input: 7, Output: 9, Total: 16},
		},
		{
			name:   "image model with no token fields",
			body:   `{"images":["base64..."]}`,
			wantOK: false,
		},
	})
}

func TestParseAzureOpenAI(t *testing.T) {
	runParseCases(t, ParseAzureOpenAI, []parseCase{
		{
			name:   "azure chat completion is openai-compatible",
			body:   `{"model":"gpt-4o","usage":{"prompt_tokens":11,"completion_tokens":22,"total_tokens":33}}`,
			wantOK: true,
			want:   Tokens{Input: 11, Output: 22, Total: 33},
		},
		{
			name:   "no usage",
			body:   `{"model":"gpt-4o","choices":[]}`,
			wantOK: false,
		},
	})
}

// TestParseAzureOpenAIDelegatesToOpenAI locks in the documented contract that
// the Azure surface reuses the OpenAI parser, so the two never silently drift.
func TestParseAzureOpenAIDelegatesToOpenAI(t *testing.T) {
	body := []byte(`{"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	azTok, azOK := ParseAzureOpenAI(body)
	oaTok, oaOK := ParseOpenAI(body)
	if azOK != oaOK || azTok != oaTok {
		t.Fatalf("azure (%+v,%v) != openai (%+v,%v)", azTok, azOK, oaTok, oaOK)
	}
}

// TestParsersRejectMalformedInput guards every parser against the inputs the
// request path can realistically hand it: empty bodies, truncated JSON, and
// non-JSON leading bytes. None may panic; all must return ok=false.
func TestParsersRejectMalformedInput(t *testing.T) {
	parsers := map[string]func([]byte) (Tokens, bool){
		"openai":       ParseOpenAI,
		"anthropic":    ParseAnthropic,
		"gemini":       ParseGemini,
		"bedrock":      ParseBedrock,
		"azure_openai": ParseAzureOpenAI,
	}
	bad := map[string]string{
		"empty":              "",
		"whitespace":         "   \n",
		"truncated json":     `{"usage":{"prompt_tokens":1`,
		"non-json leading":   `not-json`,
		"html error page":    `<html><body>502 Bad Gateway</body></html>`,
		"bare done sentinel": `data: [DONE]`,
	}
	for pname, parse := range parsers {
		for bname, body := range bad {
			t.Run(pname+"/"+bname, func(t *testing.T) {
				tok, ok := parse([]byte(body))
				if ok {
					t.Fatalf("expected ok=false, got tokens=%+v", tok)
				}
				if tok != (Tokens{}) {
					t.Fatalf("expected zero Tokens, got %+v", tok)
				}
			})
		}
	}
}

func TestTokensFinalize(t *testing.T) {
	cases := []struct {
		name string
		in   Tokens
		want Tokens
	}{
		{"computes total from parts", Tokens{Input: 5, Output: 7}, Tokens{Input: 5, Output: 7, Total: 12}},
		{"keeps explicit total", Tokens{Input: 5, Output: 7, Total: 99}, Tokens{Input: 5, Output: 7, Total: 99}},
		{"all zero stays zero", Tokens{}, Tokens{}},
		{"input only", Tokens{Input: 4}, Tokens{Input: 4, Total: 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.finalize(); got != tc.want {
				t.Fatalf("finalize() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
