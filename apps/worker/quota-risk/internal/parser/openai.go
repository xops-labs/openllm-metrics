// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package parser

import "time"

// OpenAI parses the standard OpenAI rate-limit headers.
//
// Reference (public docs, OSS-safe):
//
//	x-ratelimit-limit-requests
//	x-ratelimit-limit-tokens
//	x-ratelimit-remaining-requests
//	x-ratelimit-remaining-tokens
//	x-ratelimit-reset-requests   (duration: "1s", "6m0s", "250ms")
//	x-ratelimit-reset-tokens     (duration)
//
// We choose the LONGER of the two reset values as the canonical
// ResetAfter, because risk is dominated by whichever pool resets later.
type OpenAI struct{}

// Provider returns the canonical provider slug.
func (OpenAI) Provider() string { return "openai" }

// Parse extracts a Signal from the OpenAI response headers.
func (OpenAI) Parse(headers map[string]string) Signal {
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

	resetReq := parseDurationLike(h["x-ratelimit-reset-requests"], now)
	resetTok := parseDurationLike(h["x-ratelimit-reset-tokens"], now)
	sig.ResetAfter = pickReset(resetReq, resetTok)

	return sig
}

// pickReset returns the larger of two non-zero candidates (worst-case
// reset window). When one is zero, the other wins.
func pickReset(a, b time.Duration) time.Duration {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	case a > b:
		return a
	default:
		return b
	}
}
