// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package parser

import "time"

// Anthropic parses the Claude API rate-limit headers.
//
// Reference (public docs, OSS-safe):
//
//	anthropic-ratelimit-requests-limit
//	anthropic-ratelimit-requests-remaining
//	anthropic-ratelimit-requests-reset      (RFC3339 absolute timestamp)
//	anthropic-ratelimit-tokens-limit
//	anthropic-ratelimit-tokens-remaining
//	anthropic-ratelimit-tokens-reset        (RFC3339 absolute timestamp)
//
// In addition to the per-pool headers Anthropic publishes input-token and
// output-token pools (`anthropic-ratelimit-input-tokens-remaining` etc.).
// We collapse them into a single "tokens" view by summing the limits and
// taking the MIN of the remainings — the pool that hits zero first is the
// binding constraint.
type Anthropic struct{}

// Provider returns the canonical provider slug.
func (Anthropic) Provider() string { return "anthropic" }

// Parse extracts a Signal from the Anthropic response headers.
func (Anthropic) Parse(headers map[string]string) Signal {
	h := lower(headers)
	now := time.Now().UTC()

	var sig Signal

	// Aggregate token pools (combined + input + output) into a single view.
	var (
		tokRem    int64
		tokLim    int64
		haveToks  bool
		minRemSet bool
		minRem    int64
	)
	for _, suffix := range []string{"tokens", "input-tokens", "output-tokens"} {
		if v, ok := parseInt64(h["anthropic-ratelimit-"+suffix+"-remaining"]); ok {
			haveToks = true
			if !minRemSet || v < minRem {
				minRem = v
				minRemSet = true
			}
			tokRem += v
		}
		if v, ok := parseInt64(h["anthropic-ratelimit-"+suffix+"-limit"]); ok {
			haveToks = true
			tokLim += v
		}
	}
	if haveToks {
		sig.HasTokens = true
		// Use the worst-case (smallest) remaining of any pool — that's the
		// binding constraint for downstream callers.
		if minRemSet {
			sig.TokensRemaining = minRem
		} else {
			sig.TokensRemaining = tokRem
		}
		sig.TokensLimit = tokLim
	}

	if v, ok := parseInt64(h["anthropic-ratelimit-requests-remaining"]); ok {
		sig.RequestsRemaining = v
		sig.HasRequests = true
	}
	if v, ok := parseInt64(h["anthropic-ratelimit-requests-limit"]); ok {
		sig.RequestsLimit = v
		sig.HasRequests = true
	}

	resetReq := parseDurationLike(h["anthropic-ratelimit-requests-reset"], now)
	resetTok := parseDurationLike(h["anthropic-ratelimit-tokens-reset"], now)
	sig.ResetAfter = pickReset(resetReq, resetTok)

	return sig
}
