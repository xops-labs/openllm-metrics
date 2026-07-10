// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

// Package openaiclient is the OpenAI-specific HTTP client + response types.
//
// This package is the ONLY place in F009 that may import OpenAI-specific
// schemas. The adapter package consumes these types and produces the
// vendor-neutral llm.usage.normalized event — nothing OpenAI-shaped escapes
// the adapter boundary. Provider portability hinges on this isolation.
package openaiclient

// UsageResponse is the subset of the OpenAI Admin Usage API response the
// poller cares about. The real API exposes more fields; we deliberately do
// NOT serialize anything that could carry user content (prompts, outputs,
// embedding vectors, request bodies) per F008 §11 and F009 §11.
//
// Spec ref: https://platform.openai.com/docs/api-reference/usage
type UsageResponse struct {
	Object   string        `json:"object"`
	Data     []UsageBucket `json:"data"`
	HasMore  bool          `json:"has_more"`
	NextPage string        `json:"next_page,omitempty"`
}

// UsageBucket is a single time bucket from the Admin Usage API.
type UsageBucket struct {
	Object    string        `json:"object"`
	StartTime int64         `json:"start_time"` // unix seconds
	EndTime   int64         `json:"end_time"`
	Results   []UsageResult `json:"results"`
}

// UsageResult is one (model, project) usage row inside a bucket.
//
// We pull out ONLY the operational counters. We never read or persist any
// fields that would be a payload (prompts, completions, messages, content).
type UsageResult struct {
	Object           string `json:"object"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	NumModelRequests int64  `json:"num_model_requests"`
	ProjectID        string `json:"project_id"`
	Model            string `json:"model"`
}

// CostResponse is the subset of the OpenAI Admin Cost API response the
// poller cares about. Amounts arrive as a {value, currency} struct.
//
// Spec ref: https://platform.openai.com/docs/api-reference/usage/costs
type CostResponse struct {
	Object   string       `json:"object"`
	Data     []CostBucket `json:"data"`
	HasMore  bool         `json:"has_more"`
	NextPage string       `json:"next_page,omitempty"`
}

// CostBucket is a single time bucket from the Admin Cost API.
type CostBucket struct {
	Object    string       `json:"object"`
	StartTime int64        `json:"start_time"`
	EndTime   int64        `json:"end_time"`
	Results   []CostResult `json:"results"`
}

// CostResult is one cost row inside a bucket. Amount.Value is a USD float
// (e.g. 0.0187 means $0.0187). The adapter converts to integer minor units
// per F008 §10.
type CostResult struct {
	Object    string `json:"object"`
	Amount    Amount `json:"amount"`
	LineItem  string `json:"line_item,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
}

// Amount is the OpenAI cost amount shape.
type Amount struct {
	Value    float64 `json:"value"`
	Currency string  `json:"currency"`
}

// CombinedWindow pairs a usage and cost response for the same time range.
// The poller fetches both endpoints per cycle and the adapter joins them
// before producing normalized events.
type CombinedWindow struct {
	Usage UsageResponse
	Cost  CostResponse
}
