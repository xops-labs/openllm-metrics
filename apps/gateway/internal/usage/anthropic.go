// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import (
	"encoding/json"
)

// ParseAnthropic extracts usage from an Anthropic /v1/messages response.
//
// Non-streaming: the top-level object carries
//
//	"usage": { "input_tokens": N, "output_tokens": M }
//
// Streaming: usage appears on the `message_start` event (input only) and on
// `message_delta` (output incrementally). For runtime metrics we accept
// either shape and surface whichever fields are present; the gateway
// observer holds the most recent values until response completion.
func ParseAnthropic(body []byte) (Tokens, bool) {
	if len(body) == 0 {
		return Tokens{}, false
	}
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return Tokens{}, false
	}
	var resp struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		// `message_delta` SSE wraps usage in a nested "delta.usage" alongside
		// a top-level "usage" with output_tokens. Try the top-level path
		// first; if absent, fall back to message_start shape.
		Message *struct {
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Tokens{}, false
	}
	t := Tokens{}
	if resp.Usage != nil {
		t.Input = resp.Usage.InputTokens
		t.Output = resp.Usage.OutputTokens
	}
	if resp.Message != nil && resp.Message.Usage != nil {
		if t.Input == 0 {
			t.Input = resp.Message.Usage.InputTokens
		}
		if t.Output == 0 {
			t.Output = resp.Message.Usage.OutputTokens
		}
	}
	if t.Input == 0 && t.Output == 0 {
		return Tokens{}, false
	}
	return t.finalize(), true
}
