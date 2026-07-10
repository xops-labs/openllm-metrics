// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package parser

import "time"

// Gemini parses Google Gemini / Vertex AI quota headers.
//
// Gemini does not publish a stable per-request set of remaining/limit
// headers across all surfaces. The OSS-safe fields we DO observe are:
//
//	x-goog-quota-user                       (echoed back, not a signal)
//	x-goog-quota-limit                      (when present, requests-per-min)
//	x-goog-quota-remaining                  (when present)
//	x-goog-quota-reset                      (seconds, when present)
//	retry-after                             (seconds, on 429)
//
// Vertex AI may instead surface quota in the response body or via Cloud
// Monitoring; this parser handles the header-only case. When the only
// signal is `retry-after`, we encode it as a request-quota saturation
// (remaining = 0, limit unknown) so downstream sees a clear "throttling
// in progress" event.
type Gemini struct{}

// Provider returns the canonical provider slug.
func (Gemini) Provider() string { return "google" }

// Parse extracts a Signal from the Gemini/Vertex response headers.
func (Gemini) Parse(headers map[string]string) Signal {
	h := lower(headers)
	now := time.Now().UTC()

	var sig Signal

	if v, ok := parseInt64(h["x-goog-quota-remaining"]); ok {
		sig.RequestsRemaining = v
		sig.HasRequests = true
	}
	if v, ok := parseInt64(h["x-goog-quota-limit"]); ok {
		sig.RequestsLimit = v
		sig.HasRequests = true
	}

	// Reset hint preference: explicit quota reset > Retry-After.
	reset := parseDurationLike(h["x-goog-quota-reset"], now)
	if reset == 0 {
		reset = parseDurationLike(h["retry-after"], now)
		// When the ONLY signal is Retry-After, infer "exhausted" so the
		// risk model can light up. Limit stays zero (unknown).
		if reset > 0 && !sig.HasRequests {
			sig.HasRequests = true
			sig.RequestsRemaining = 0
			sig.RequestsLimit = 0
		}
	}
	sig.ResetAfter = reset

	return sig
}
