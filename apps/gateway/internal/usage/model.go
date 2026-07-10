// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

import (
	"encoding/json"
)

// Provider identifiers accepted by ParseModel. These mirror the string
// values the observer uses; kept local so the usage package has no import
// dependency on the observer package.
const (
	providerOpenAI      = "openai"
	providerAnthropic   = "anthropic"
	providerGemini      = "google"
	providerBedrock     = "bedrock"
	providerAzureOpenAI = "azure_openai"
)

// ParseModel extracts the model identifier from a provider response body.
//
// PRIVACY INVARIANT — like the token parsers, ParseModel operates on bytes
// already streamed to the client and only the extracted model STRING (a
// scalar label, never a prompt/completion) leaves this package. It never
// logs or persists the body.
//
// For OpenAI, Azure-OpenAI, and Anthropic the model name travels in the
// response body (and, for streaming, in chunks), so it is parsed here.
// Gemini and Bedrock surface the model in the request URL path, so the
// observer already has it from the route — ParseModel returns "" for them.
//
// Returns "" when the model is absent or the body is unparseable; never
// errors.
func ParseModel(provider string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	switch provider {
	case providerOpenAI, providerAzureOpenAI:
		return parseOpenAIModel(body)
	case providerAnthropic:
		return parseAnthropicModel(body)
	case providerGemini, providerBedrock:
		// Model comes from the URL path; nothing to extract from the body.
		return ""
	default:
		return ""
	}
}

// parseOpenAIModel reads the top-level "model" field present in OpenAI (and
// OpenAI-shaped Azure) non-streaming responses AND in every streaming
// chunk. Azure reuses this implementation since its body is OpenAI-shaped.
func parseOpenAIModel(body []byte) string {
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return ""
	}
	var resp struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	return resp.Model
}

// parseAnthropicModel reads the model from an Anthropic /v1/messages
// response. The non-streaming response carries a top-level "model"; the
// streaming "message_start" event carries it under message.model (the
// trailing "message_delta" usage chunk does NOT). Both shapes are tried.
func parseAnthropicModel(body []byte) string {
	body = stripSSEPrefix(body)
	if len(body) == 0 || body[0] != '{' {
		return ""
	}
	var resp struct {
		Model   string `json:"model"`
		Message *struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	if resp.Model != "" {
		return resp.Model
	}
	if resp.Message != nil {
		return resp.Message.Model
	}
	return ""
}
