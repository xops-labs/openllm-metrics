// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package parser

import "time"

// AzureOpenAI parses Azure OpenAI Service rate-limit headers.
//
// Reference (public docs, OSS-safe):
//
//	x-ratelimit-remaining-tokens
//	x-ratelimit-remaining-requests
//	x-ratelimit-limit-tokens          (sometimes absent depending on tier)
//	x-ratelimit-limit-requests        (sometimes absent depending on tier)
//	retry-after-ms                    (preferred; milliseconds)
//	retry-after                       (seconds, fallback)
//
// Azure does not always expose `*-limit-*` headers; when the limit is
// missing, the parser leaves Limit = 0 and the model treats Limit=0 as
// "denominator unknown" (the risk gauge is skipped for that kind but
// secondsToReset is still emitted).
type AzureOpenAI struct{}

// Provider returns the canonical provider slug.
func (AzureOpenAI) Provider() string { return "azure_openai" }

// Parse extracts a Signal from the Azure OpenAI response headers.
func (AzureOpenAI) Parse(headers map[string]string) Signal {
	h := lower(headers)
	now := time.Now().UTC()

	var sig Signal

	if v, ok := parseInt64(h["x-ratelimit-remaining-tokens"]); ok {
		sig.TokensRemaining = v
		sig.HasTokens = true
	}
	if v, ok := parseInt64(h["x-ratelimit-limit-tokens"]); ok {
		sig.TokensLimit = v
		sig.HasTokens = true
	}
	if v, ok := parseInt64(h["x-ratelimit-remaining-requests"]); ok {
		sig.RequestsRemaining = v
		sig.HasRequests = true
	}
	if v, ok := parseInt64(h["x-ratelimit-limit-requests"]); ok {
		sig.RequestsLimit = v
		sig.HasRequests = true
	}

	// retry-after-ms wins over retry-after when both are present.
	if v, ok := parseInt64(h["retry-after-ms"]); ok && v >= 0 {
		sig.ResetAfter = time.Duration(v) * time.Millisecond
	} else {
		sig.ResetAfter = parseDurationLike(h["retry-after"], now)
	}

	return sig
}
