// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package parser

import "time"

// Bedrock parses AWS Bedrock rate-limit headers.
//
// Reference (public docs, OSS-safe):
//
//	x-amzn-bedrock-input-token-count     (echoed, not a signal)
//	x-amzn-bedrock-output-token-count    (echoed, not a signal)
//	x-amzn-RateLimit-Limit               (requests per minute, when present)
//	x-amzn-errortype                     (categorical)
//	retry-after                          (seconds, on ThrottlingException)
//
// Bedrock does not consistently publish a "remaining" header — the
// authoritative signal is throttling exceptions plus Retry-After. When we
// see ONLY x-amzn-RateLimit-Limit + retry-after, we mark the request pool
// as saturated (remaining = 0) so the risk model lights up; otherwise we
// emit the limit alone (no risk computation possible without remaining).
type Bedrock struct{}

// Provider returns the canonical provider slug.
func (Bedrock) Provider() string { return "bedrock" }

// Parse extracts a Signal from the AWS Bedrock response headers.
func (Bedrock) Parse(headers map[string]string) Signal {
	h := lower(headers)
	now := time.Now().UTC()

	var sig Signal

	if v, ok := parseInt64(h["x-amzn-ratelimit-limit"]); ok {
		sig.RequestsLimit = v
		sig.HasRequests = true
	}

	retryAfter := parseDurationLike(h["retry-after"], now)
	sig.ResetAfter = retryAfter

	// If we see Retry-After plus a known limit, the pool is exhausted.
	// If we see Retry-After without a known limit, signal saturation
	// without a denominator (downstream can still compute secondsToReset).
	if retryAfter > 0 {
		sig.HasRequests = true
		sig.RequestsRemaining = 0
	}

	return sig
}
