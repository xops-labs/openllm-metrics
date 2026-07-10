// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package usage extracts prompt/completion token counts from each
// provider's response shape (or the streaming `usage` chunk).
//
// PRIVACY INVARIANT — these parsers operate on response bytes that have
// already been streamed to the client. They MUST NOT log, persist, or
// telemeter the response body itself. Only the integer usage fields leave
// this package. If a field is absent the parser returns (Tokens{}, false)
// without an error — Tokens are optional in the runtime event.
package usage

import (
	"bytes"
	"encoding/json"
)

// Tokens is the normalized token-count triple. Total is computed when
// absent in the provider response.
type Tokens struct {
	Input  int
	Output int
	Total  int
}

func (t Tokens) finalize() Tokens {
	if t.Total == 0 && (t.Input > 0 || t.Output > 0) {
		t.Total = t.Input + t.Output
	}
	return t
}

// ParseOpenAI extracts usage from an OpenAI non-streaming or streaming
// final chunk. OpenAI returns `usage: { prompt_tokens, completion_tokens,
// total_tokens }` in chat.completions, embeddings, and (with
// stream_options.include_usage=true) on the final streaming chunk.
func ParseOpenAI(body []byte) (Tokens, bool) {
	if len(body) == 0 {
		return Tokens{}, false
	}
	// Strip a leading "data: " prefix if the caller handed us a raw SSE
	// line (defensive — streaming.go is normally responsible for slicing).
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return Tokens{}, false
	}
	var resp struct {
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
			// /v1/responses uses input_tokens / output_tokens.
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Tokens{}, false
	}
	if resp.Usage == nil {
		return Tokens{}, false
	}
	t := Tokens{
		Input:  pickPositive(resp.Usage.PromptTokens, resp.Usage.InputTokens),
		Output: pickPositive(resp.Usage.CompletionTokens, resp.Usage.OutputTokens),
		Total:  resp.Usage.TotalTokens,
	}
	if t.Input == 0 && t.Output == 0 && t.Total == 0 {
		return Tokens{}, false
	}
	return t.finalize(), true
}

func pickPositive(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

func stripSSEPrefix(b []byte) []byte {
	b = bytes.TrimSpace(b)
	if bytes.HasPrefix(b, []byte("data:")) {
		b = bytes.TrimSpace(b[len("data:"):])
	}
	if bytes.Equal(b, []byte("[DONE]")) {
		return nil
	}
	return b
}
