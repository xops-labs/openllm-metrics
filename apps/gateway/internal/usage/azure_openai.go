// Copyright 2026 Yasvanth Udayakumar
// Licensed under the Apache License, Version 2.0.

package usage

// ParseAzureOpenAI extracts usage from an Azure OpenAI chat-completions
// response. The Azure surface is OpenAI-compatible: same `usage` object
// with `prompt_tokens`, `completion_tokens`, `total_tokens`. We delegate
// to the OpenAI parser rather than re-implement it; the dedicated file
// exists so the per-provider dispatch in observer.go reads symmetrically
// and so a future Azure-only divergence (e.g., content-filter result
// counts) has a clear home.
func ParseAzureOpenAI(body []byte) (Tokens, bool) {
	return ParseOpenAI(body)
}
