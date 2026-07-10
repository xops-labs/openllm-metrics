// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import (
	"encoding/json"
)

// ParseGemini extracts usage from a Google Gemini `:generateContent`
// response. The payload carries:
//
//	"usageMetadata": {
//	    "promptTokenCount": N,
//	    "candidatesTokenCount": M,
//	    "totalTokenCount": N+M
//	}
//
// Streaming `:streamGenerateContent` emits a usageMetadata block on the
// final chunk; this parser accepts either shape and returns ok=false if
// the metadata is absent (a partial streaming chunk).
func ParseGemini(body []byte) (Tokens, bool) {
	if len(body) == 0 {
		return Tokens{}, false
	}
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return Tokens{}, false
	}
	var resp struct {
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return Tokens{}, false
	}
	if resp.UsageMetadata == nil {
		return Tokens{}, false
	}
	t := Tokens{
		Input:  resp.UsageMetadata.PromptTokenCount,
		Output: resp.UsageMetadata.CandidatesTokenCount,
		Total:  resp.UsageMetadata.TotalTokenCount,
	}
	if t.Input == 0 && t.Output == 0 && t.Total == 0 {
		return Tokens{}, false
	}
	return t.finalize(), true
}
